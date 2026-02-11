#!/usr/bin/env python3
# AI-Attribution: AIA EAI Hin R gemini-3.0-pro v1.0
# SPDX-License-Identifier: Apache-2.0

import sys
import re
import xml.etree.ElementTree as ET
from typing import BinaryIO

# The name of the secondary network dedicated to SR-IOV traffic
SRIOV_NETWORK_NAME = "sriov-net"

def ensure_machine_type(root: ET.Element) -> bool:
    """
    Ensures the machine type is q35, preserving version suffixes.
    e.g., 'pc-i440fx-7.1' -> 'pc-q35-7.1'
    """
    type_elem = root.find("./os/type")
    if type_elem is None:
        return False

    current_machine = type_elem.get("machine", "")
    
    # If it's already a q35 type, we are good
    if "q35" in current_machine:
        return False

    # Regex to capture version suffix (e.g. "-7.1", "-8.0")
    version_match = re.search(r'(-(\d+(\.\d+)+))$', current_machine)

    new_machine = "q35"
    if version_match:
        version_suffix = version_match.group(1)
        new_machine = f"pc-q35{version_suffix}"

    if new_machine != current_machine:
        type_elem.set("machine", new_machine)
        return True
    
    return False

def ensure_ioapic(root: ET.Element) -> bool:
    """Ensures <ioapic driver='qemu'/> exists in <features>."""
    modified = False
    features = root.find("features")
    if features is None:
        features = ET.SubElement(root, "features")
        modified = True
    
    ioapic = features.find("ioapic")
    if ioapic is None:
        ioapic = ET.SubElement(features, "ioapic")
        modified = True
    
    if ioapic.get("driver") != "qemu":
        ioapic.set("driver", "qemu")
        modified = True
        
    return modified

def ensure_iommu(root: ET.Element) -> bool:
    """Ensures <iommu model='intel'> with driver settings exists in <devices>."""
    modified = False
    devices = root.find("devices")
    if devices is None:
        devices = ET.SubElement(root, "devices")
        modified = True

    iommu = devices.find("iommu")
    if iommu is None:
        iommu = ET.SubElement(devices, "iommu")
        modified = True
    
    if iommu.get("model") != "intel":
        iommu.set("model", "intel")
        modified = True

    driver = iommu.find("driver")
    if driver is None:
        driver = ET.SubElement(iommu, "driver")
        modified = True

    if driver.get("intremap") != "on":
        driver.set("intremap", "on")
        modified = True
    
    if driver.get("iotlb") != "on":
        driver.set("iotlb", "on")
        modified = True

    return modified

def ensure_igb_interface(root: ET.Element) -> bool:
    """
    Ensures an interface with model type='igb' exists, attached to the SRIOV network.
    """
    modified = False
    devices = root.find("devices")
    if devices is None:
        devices = ET.SubElement(root, "devices")
        modified = True

    # Check if an igb interface already exists to avoid duplicates
    # We search specifically for a network interface with model type='igb'
    for interface in devices.findall("interface"):
        if interface.get("type") == "network":
            model = interface.find("model")
            if model is not None and model.get("type") == "igb":
                # Already exists, assuming strictly one igb device is needed
                return False

    # Create the new interface
    # <interface type='network'>
    #   <source network='sriov-net'/>
    #   <model type='igb'/>
    # </interface>
    new_interface = ET.SubElement(devices, "interface", type="network")
    ET.SubElement(new_interface, "source", network=SRIOV_NETWORK_NAME)
    ET.SubElement(new_interface, "model", type="igb")
    
    return True

def process_xml_stream(input_stream: BinaryIO, output_stream: BinaryIO) -> int:
    try:
        xml_data = input_stream.read()
        if not xml_data:
            return 0

        root = ET.fromstring(xml_data)

        # 1. Check Domain Name - the control plane is usually just named
        # `minikube`, then the worker nodes come.
        name_elem = root.find("name")
        if name_elem is not None and name_elem.text and name_elem.text.startswith("minikube-m"):

            changes_made = False

            # Apply all mutations
            if ensure_machine_type(root): changes_made = True
            if ensure_ioapic(root): changes_made = True
            if ensure_iommu(root): changes_made = True
            if ensure_igb_interface(root): changes_made = True

            if changes_made:
                tree = ET.ElementTree(root)
                tree.write(output_stream, encoding="UTF-8", xml_declaration=True)
                return 0

        # Fallback: No changes needed
        output_stream.write(xml_data)
        return 0

    except ET.ParseError:
        sys.stderr.write("Error: Failed to parse XML input.\n")
        return 1
    except Exception as e:
        sys.stderr.write(f"Error: {e}\n")
        return 1

def main() -> int:
    # Logic only runs on 'prepare' -> 'begin'
    if len(sys.argv) >= 5:
        operation = sys.argv[2]
        sub_operation = sys.argv[3]
        if operation != "prepare" or sub_operation != "begin":
            return 0

    return process_xml_stream(sys.stdin.buffer, sys.stdout.buffer)

if __name__ == "__main__":
    sys.exit(main())
