# virtinjectd

A transparent libvirt RPC proxy that intercepts `virDomainDefineXML` calls
and passes the domain XML through a chain of hook scripts before forwarding
to the real libvirtd daemon.

## Why this exists

libvirt provides a [hook mechanism](https://libvirt.org/hooks.html) that
lets administrators run scripts at various points in a VM's lifecycle.
However, for the `prepare` and `start` phases of QEMU domains, **libvirtd
discards the hook script's stdout**. The hook can abort the operation by
returning a non-zero exit code, but it cannot modify the domain XML.

This is a known limitation in `qemuProcessStartHook()` in the libvirt
source (`src/qemu/qemu_process.c`), where `virHookCall()` is invoked with
`NULL` as the output parameter. Compare with the `migrate` and `restore`
hooks, which do capture and use the output.

This proxy works around the limitation by intercepting the
`virDomainDefineXML` RPC call at the wire protocol level, running the hook
chain, and forwarding the patched XML to libvirtd. Since the proxy is
opt-in (you must explicitly point clients at its socket), there is no risk
of breaking existing workflows.

## Building

From the parent directory:

```bash
make build-virtinjectd
```

Or directly:

```bash
cd virtinjectd
go build -o virtinjectd .
```

## Usage

### Basic usage with a single hook

```bash
./virtinjectd \
  --listen /tmp/libvirt-proxy.sock \
  --upstream /var/run/libvirt/libvirt-sock \
  --hook /path/to/dramachine.py
```

### Usage with a hook directory

```bash
./virtinjectd \
  --listen /tmp/libvirt-proxy.sock \
  --upstream /var/run/libvirt/libvirt-sock \
  --hook-dir /path/to/hooks.d/
```

### Usage with both a single hook and a directory

```bash
./virtinjectd \
  --listen /tmp/libvirt-proxy.sock \
  --hook /path/to/main-hook.py \
  --hook-dir /path/to/hooks.d/
```

The single hook runs first, then the directory hooks in alphabetical order.

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `/tmp/libvirt-proxy.sock` | Path for the proxy Unix socket |
| `--upstream` | `/var/run/libvirt/libvirt-sock` | Path to the real libvirtd socket |
| `--hook` | (none) | Path to a single hook script |
| `--hook-dir` | (none) | Path to a directory of hook scripts |
| `-v` | `0` | Logging verbosity (0=silent, 1=info, 2=debug, 3=trace, 4=trace+XML) |

At least one of `--hook` or `--hook-dir` must be specified.

## Using with minikube

Start virtinjectd in one terminal:

```bash
./virtinjectd \
  --listen /tmp/libvirt-proxy.sock \
  --hook ./scripts/libvirt-qemu-hook/dramachine.py \
  -v 1
```

Then start minikube with the proxy URI in another terminal:

```bash
minikube start \
  --feature-gates=DRAResourceClaimDeviceStatus=true,DRAConsumableCapacity=true,DRAPartitionableDevices=true \
  --container-runtime=containerd \
  --nodes=2 \
  --driver=kvm2 \
  --kvm-qemu-uri='qemu+unix:///system?socket=/tmp/libvirt-proxy.sock' \
  --kvm-numa-count=2 \
  --cpus=16 \
  --memory=16g
```

Or with the Makefile:

```bash
make minikube-create LIBVIRT_PROXY_SOCK=/tmp/libvirt-proxy.sock
```

## Hook script interface

Hook scripts are invoked with the **exact same calling convention** as
libvirt's `/etc/libvirt/hooks/qemu` hooks:

```
/path/to/hook "virtinjectd" "prepare" "begin" "-"
```

### Arguments

| argv | Value | Description |
|------|-------|-------------|
| `argv[0]` | script path | The hook script itself |
| `argv[1]` | `"virtinjectd"` | Domain name (see note below) |
| `argv[2]` | `"prepare"` | Operation |
| `argv[3]` | `"begin"` | Sub-operation |
| `argv[4]` | `"-"` | Extra arguments |

### stdin / stdout

- **stdin**: The full domain XML
- **stdout**: The modified domain XML. Empty output means "no changes"
  (the original XML is passed through unchanged).
- **exit 0**: Success
- **non-zero exit**: Failure. The error is logged, but the original XML
  is preserved and execution continues with the next hook in the chain.

### Important: domain name is "virtinjectd" by default

The domain name passed as `argv[1]` is **always hardcoded** and the value is
the string `"virtinjectd"`by default. You can change this value with command
line flags, but the **value is constant per process run**.
This differs from real libvirt hooks where `argv[1]` is the guest name.

The reason: virtinjectd intercepts at the RPC level, where the domain name
is only available inside the XML payload, not in the RPC message metadata.
Rather than parsing the XML twice (once to extract the name, once in the
hook), the proxy uses a fixed sentinel value.

**Hooks that need the domain name should extract it from the XML on stdin**,
not from `argv[1]`. The existing `dramachine.py` hook works correctly
because it reads the `<name>` element from the XML content.

### Hook chaining

When both `--hook` and `--hook-dir` are used:

1. The single hook script runs first
2. Directory hooks run next, in alphabetical order (sorted by filename)
3. Each hook's stdout becomes the next hook's stdin
4. Empty stdout from any hook = pass the current XML through unchanged

This matches the behavior of libvirt's `qemu.d/` directory hooks
(available since libvirt 6.5.0).

## Compatibility with existing hooks

The `scripts/libvirt-qemu-hook/dramachine.py` hook works unchanged with
this proxy. The hook's `main()` function checks `argv[2] == "prepare"`
and `argv[3] == "begin"` for filtering (which match the proxy's values),
and reads the domain name from the XML `<name>` element (not from
`argv[1]`). No modifications are needed.

## How it works

virtinjectd implements a subset of the
[libvirt RPC protocol](https://libvirt.org/kbase/internals/rpc.html):

1. It listens on a Unix socket for client connections
2. For each client, it opens a paired connection to the real libvirtd
3. All RPC messages are forwarded transparently as raw bytes
4. Only `REMOTE_PROC_DOMAIN_DEFINE_XML` (procedure 11) and
   `REMOTE_PROC_DOMAIN_DEFINE_XML_FLAGS` (procedure 350) are intercepted
5. For intercepted calls: the XDR-encoded domain XML is decoded, passed
   through the hook chain, re-encoded, and forwarded with the updated payload

Everything else (keepalives, events, streams, other API calls) passes
through as opaque bytes without parsing.

## Licensing and provenance

virtinjectd is licensed under **LGPL-2.1-or-later**.

### Code provenance

virtinjectd is a **clean-room Go implementation**. No C code from libvirt
is copied, translated, or adapted. However, certain numeric constants and
structural knowledge are derived from libvirt source files:

**From `src/remote/remote_protocol.x`** (LGPL-2.1-or-later, Copyright Red Hat, Inc.):
- `REMOTE_PROGRAM = 0x20008086`
- `REMOTE_PROTOCOL_VERSION = 1`
- `REMOTE_PROC_DOMAIN_DEFINE_XML = 11`
- `REMOTE_PROC_DOMAIN_DEFINE_XML_FLAGS = 350`
- `REMOTE_STRING_MAX = 4194304`
- XDR structure of `remote_domain_define_xml_args` and
  `remote_domain_define_xml_flags_args`

**From `src/rpc/virnetprotocol.x`** (LGPL-2.1-or-later, Copyright Red Hat, Inc.):
- `VIR_NET_MESSAGE_HEADER_MAX = 24`
- `VIR_NET_MESSAGE_LEN_MAX = 4`
- `VIR_NET_MESSAGE_MAX = 33554432`
- `virNetMessageHeader` structure (6 x uint32)
- `virNetMessageType` enum values

**From public documentation** (not source code):
- Wire protocol framing: https://libvirt.org/kbase/internals/rpc.html
- Hook calling convention: https://libvirt.org/hooks.html
- XDR encoding: RFC 4506

**Entirely original:**
- All Go source code
- Proxy architecture and forwarding logic
- Hook chaining implementation

### Why LGPL-2.1+

While there is precedent for reimplementing libvirt protocol support under
permissive licenses (the official Go bindings `libvirt.org/go/libvirt` are
MIT; `github.com/digitalocean/go-libvirt` is Apache-2.0), we err on the
side of caution. The LGPL-2.1+ license matches the libvirt protocol
definition files and avoids any ambiguity.

The parent project (`dra-driver-integration-env`) is Apache-2.0.
virtinjectd is a standalone Go module with its own `go.mod` and `LICENSE` file.
