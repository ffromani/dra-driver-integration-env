// AI-Attribution: AIA PAI Ce Hin R gpt-5.3-codex v1.0
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultSysfsRoot = "/sys"
	defaultNumVFs    = 4
)

type config struct {
	sysfsRoot string
	numVFs    int
	verbosity string
	pciAddr   string
	tryMode   bool
}

type pfDevice struct {
	pciAddr    string
	path       string
	totalVFs   int
	currentVFs int
}

func main() {
	cfg := parseFlags()
	logger, err := newLogger(cfg.verbosity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to configure logger: %v\n", err)
		os.Exit(2)
	}

	if err := run(cfg, logger); err != nil {
		logger.Error("operation failed", "error", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	cfg := config{}
	flag.Usage = func() {
		base := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [pf_pci_address]\n", base)
		flag.PrintDefaults()
	}
	flag.StringVar(&cfg.sysfsRoot, "sysfs", defaultSysfsRoot, "sysfs mount point to use")
	flag.IntVar(&cfg.numVFs, "num-vfs", defaultNumVFs, "number of virtual functions to create")
	flag.StringVar(&cfg.verbosity, "verbosity", "info", "log verbosity: quiet|info|debug")
	flag.BoolVar(&cfg.tryMode, "try", false, "attempt operations and always exit successfully even on failures")
	flag.Parse()

	args := flag.Args()
	if len(args) > 1 {
		flag.Usage()
		os.Exit(2)
	}
	if len(args) == 1 {
		cfg.pciAddr = args[0]
	}
	return cfg
}

func newLogger(verbosity string) (*slog.Logger, error) {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(verbosity)) {
	case "quiet", "error":
		level = slog.LevelError
	case "info", "normal":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	default:
		return nil, fmt.Errorf("unsupported verbosity %q (expected quiet|info|debug)", verbosity)
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(handler), nil
}

func run(cfg config, logger *slog.Logger) error {
	hadFailures := false
	handleFailure := func(step string, err error) error {
		if err == nil {
			return nil
		}
		if !cfg.tryMode {
			return err
		}
		hadFailures = true
		logger.Error("try mode: step failed, continuing", "step", step, "error", err)
		return nil
	}

	if cfg.numVFs < 0 {
		if err := handleFailure("validate --num-vfs", fmt.Errorf("--num-vfs must be >= 0, got %d", cfg.numVFs)); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}
	if err := ensureDir(cfg.sysfsRoot); err != nil {
		if err := handleFailure("validate --sysfs", fmt.Errorf("invalid sysfs root %q: %w", cfg.sysfsRoot, err)); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}

	pf, err := resolvePF(cfg.sysfsRoot, cfg.pciAddr, logger)
	if err != nil {
		if err := handleFailure("resolve PF device", err); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}

	logger.Info("selected SR-IOV PF device", "pci", pf.pciAddr, "path", pf.path, "total_vfs", pf.totalVFs, "current_vfs", pf.currentVFs)

	if cfg.numVFs > pf.totalVFs {
		if err := handleFailure("validate requested VF count", fmt.Errorf("requested %d VFs exceeds device maximum (%d) for %s", cfg.numVFs, pf.totalVFs, pf.pciAddr)); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}
	if err := setNumVFs(pf, cfg.numVFs, logger); err != nil {
		if err := handleFailure("configure sriov_numvfs", err); err != nil {
			return err
		}
	}

	vfPaths, err := listVFPaths(pf.path)
	if err != nil {
		if err := handleFailure("discover VF devices", fmt.Errorf("failed to discover VFs under %s: %w", pf.path, err)); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}
	if len(vfPaths) == 0 || cfg.numVFs == 0 {
		logger.Info("no VF NUMA balancing needed", "created_vfs", len(vfPaths), "requested_vfs", cfg.numVFs)
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}

	nodes, err := listNUMANodes(cfg.sysfsRoot)
	if err != nil {
		if err := handleFailure("discover NUMA nodes", fmt.Errorf("failed to list NUMA nodes: %w", err)); err != nil {
			return err
		}
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}
	if len(nodes) == 0 {
		logger.Info("no online NUMA nodes found in sysfs, skipping VF NUMA balancing")
		return finishTryMode(cfg.tryMode, hadFailures, logger)
	}

	logger.Info("applying VF NUMA balancing", "vf_count", len(vfPaths), "numa_nodes", nodes)
	if err := balanceVFNUMA(vfPaths, nodes, logger); err != nil {
		if err := handleFailure("set VF NUMA affinity", err); err != nil {
			return err
		}
	}

	return finishTryMode(cfg.tryMode, hadFailures, logger)
}

func resolvePF(sysfsRoot, userPCI string, logger *slog.Logger) (pfDevice, error) {
	if userPCI != "" {
		if !isPCIAddress(userPCI) {
			return pfDevice{}, fmt.Errorf("invalid PCI address format %q", userPCI)
		}
		pf, err := validatePFDevice(filepath.Join(sysfsRoot, "bus", "pci", "devices"), userPCI, logger)
		if err != nil {
			return pfDevice{}, err
		}
		return pf, nil
	}

	devRoot := filepath.Join(sysfsRoot, "bus", "pci", "devices")
	entries, err := os.ReadDir(devRoot)
	if err != nil {
		return pfDevice{}, fmt.Errorf("failed to list PCI devices in %s: %w", devRoot, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		candidate := entry.Name()
		if !isPCIAddress(candidate) {
			continue
		}
		pf, err := validatePFDevice(devRoot, candidate, logger)
		if err == nil {
			return pf, nil
		}
		logger.Debug("skipping PCI device", "pci", candidate, "reason", err)
	}

	return pfDevice{}, errors.New("no SR-IOV-capable PF network device found")
}

func validatePFDevice(devRoot, pci string, logger *slog.Logger) (pfDevice, error) {
	devPath := filepath.Join(devRoot, pci)
	if err := ensureDir(devPath); err != nil {
		return pfDevice{}, fmt.Errorf("PCI device %s not found: %w", pci, err)
	}

	if _, err := os.Stat(filepath.Join(devPath, "physfn")); err == nil {
		return pfDevice{}, fmt.Errorf("PCI device %s is a VF, not a PF", pci)
	} else if !errors.Is(err, os.ErrNotExist) {
		return pfDevice{}, fmt.Errorf("failed to inspect PF/VF role of %s: %w", pci, err)
	}

	if err := ensureDir(filepath.Join(devPath, "net")); err != nil {
		return pfDevice{}, fmt.Errorf("PCI device %s is not a network interface device", pci)
	}

	totalVFs, err := readIntFromFile(filepath.Join(devPath, "sriov_totalvfs"))
	if err != nil {
		return pfDevice{}, fmt.Errorf("PCI device %s is not SR-IOV capable: %w", pci, err)
	}
	if totalVFs <= 0 {
		return pfDevice{}, fmt.Errorf("PCI device %s reports no SR-IOV capacity", pci)
	}

	currentVFs, err := readIntFromFile(filepath.Join(devPath, "sriov_numvfs"))
	if err != nil {
		return pfDevice{}, fmt.Errorf("failed reading current VF count for %s: %w", pci, err)
	}

	logger.Debug("validated SR-IOV PF", "pci", pci, "total_vfs", totalVFs, "current_vfs", currentVFs)
	return pfDevice{
		pciAddr:    pci,
		path:       devPath,
		totalVFs:   totalVFs,
		currentVFs: currentVFs,
	}, nil
}

func setNumVFs(pf pfDevice, requested int, logger *slog.Logger) error {
	target := filepath.Join(pf.path, "sriov_numvfs")
	if pf.currentVFs == requested {
		logger.Info("VF count already configured", "pci", pf.pciAddr, "num_vfs", requested)
		return nil
	}

	if pf.currentVFs > 0 {
		logger.Info("resetting existing VFs before reconfiguration", "pci", pf.pciAddr, "existing_vfs", pf.currentVFs)
		if err := writeIntToFile(target, 0); err != nil {
			return fmt.Errorf("failed to reset VFs for %s: %w", pf.pciAddr, err)
		}
	}

	logger.Info("configuring requested VFs", "pci", pf.pciAddr, "num_vfs", requested)
	if err := writeIntToFile(target, requested); err != nil {
		return fmt.Errorf("failed to configure %d VFs for %s: %w", requested, pf.pciAddr, err)
	}
	return nil
}

func listVFPaths(pfPath string) ([]string, error) {
	entries, err := os.ReadDir(pfPath)
	if err != nil {
		return nil, err
	}
	var vfPaths []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "virtfn") {
			continue
		}
		linkPath := filepath.Join(pfPath, entry.Name())
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", linkPath, err)
		}
		vfPaths = append(vfPaths, resolved)
	}
	sort.Strings(vfPaths)
	return vfPaths, nil
}

func listNUMANodes(sysfsRoot string) ([]int, error) {
	nodeRoot := filepath.Join(sysfsRoot, "devices", "system", "node")
	entries, err := os.ReadDir(nodeRoot)
	if err != nil {
		return nil, err
	}
	var nodes []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "node") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimPrefix(name, "node"))
		if err != nil {
			continue
		}
		onlinePath := filepath.Join(nodeRoot, name, "online")
		if _, err := os.Stat(onlinePath); err == nil {
			online, err := readIntFromFile(onlinePath)
			if err != nil || online == 0 {
				continue
			}
		}
		nodes = append(nodes, idx)
	}
	sort.Ints(nodes)
	return nodes, nil
}

func balanceVFNUMA(vfPaths []string, nodes []int, logger *slog.Logger) error {
	for idx, vfPath := range vfPaths {
		node := nodes[idx%len(nodes)]
		numaPath := filepath.Join(vfPath, "numa_node")
		logger.Debug("setting VF NUMA node", "vf_path", vfPath, "node", node)
		if err := writeIntToFile(numaPath, node); err != nil {
			return fmt.Errorf("failed to set VF %s numa_node=%d: %w", vfPath, node, err)
		}
		logger.Info("set VF NUMA node", "vf_path", vfPath, "node", node)
	}
	return nil
}

func ensureDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	return nil
}

func readIntFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func writeIntToFile(path string, value int) error {
	data := []byte(strconv.Itoa(value) + "\n")
	return os.WriteFile(path, data, fs.FileMode(0))
}

func isPCIAddress(addr string) bool {
	parts := strings.Split(addr, ":")
	if len(parts) != 3 {
		return false
	}
	if !isFixedHex(parts[0], 4) || !isFixedHex(parts[1], 2) {
		return false
	}
	last := strings.Split(parts[2], ".")
	if len(last) != 2 {
		return false
	}
	return isFixedHex(last[0], 2) && isFixedHex(last[1], 1)
}

func isFixedHex(s string, width int) bool {
	if len(s) != width {
		return false
	}
	for _, ch := range s {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func finishTryMode(tryMode, hadFailures bool, logger *slog.Logger) error {
	if !tryMode {
		return nil
	}
	if hadFailures {
		logger.Info("try mode: attempted all possible steps but could not fully succeed")
		return nil
	}
	logger.Info("try mode: operation succeeded")
	return nil
}
