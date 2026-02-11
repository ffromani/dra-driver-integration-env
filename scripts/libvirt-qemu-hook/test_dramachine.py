# AI-Attribution: AIA EAI Hin R gemini-3.0-pro v1.0
# SPDX-License-Identifier: Apache-2.0

import unittest
import unittest.mock
import io
import xml.etree.ElementTree as ET
from dramachine import (
    process_xml_stream, 
    ensure_machine_type, 
    ensure_ioapic, 
    ensure_iommu, 
    ensure_igb_interface,
    SRIOV_NETWORK_NAME
)

# The exact XML provided by the user (Renamed from VERBATIM_XML)
SAMPLE_DOMAIN_XML = b"""<domain type='kvm' id='2'>
  <name>minikube-m02</name>
  <uuid>1103564a-f49b-4f2f-8769-5f558680938d</uuid>
  <memory unit='KiB'>16777216</memory>
  <currentMemory unit='KiB'>16777216</currentMemory>
  <vcpu placement='static'>16</vcpu>
  <resource>
    <partition>/machine</partition>
  </resource>
  <os>
    <type arch='x86_64' machine='pc-i440fx-10.1'>hvm</type>
    <boot dev='cdrom'/>
    <boot dev='hd'/>
    <bootmenu enable='no'/>
  </os>
  <features>
    <acpi/>
    <apic/>
    <pae/>
  </features>
  <cpu mode='host-passthrough' check='none' migratable='on'>
    <numa>
      <cell id='0' cpus='0-7' memory='8388608' unit='KiB'/>
      <cell id='1' cpus='8-15' memory='8388608' unit='KiB'/>
    </numa>
  </cpu>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='/home/fromani/.minikube/machines/minikube-m02/boot2docker.iso' index='2'/>
      <backingStore/>
      <target dev='hdc' bus='scsi'/>
      <readonly/>
      <alias name='scsi0-0-2'/>
      <address type='drive' controller='0' bus='0' target='0' unit='2'/>
    </disk>
    <disk type='file' device='disk'>
      <driver name='qemu' type='raw' io='threads'/>
      <source file='/home/fromani/.minikube/machines/minikube-m02/minikube-m02.rawdisk' index='1'/>
      <backingStore/>
      <target dev='hda' bus='virtio'/>
      <alias name='virtio-disk0'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x05' function='0x0'/>
    </disk>
    <controller type='usb' index='0' model='piix3-uhci'>
      <alias name='usb'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x01' function='0x2'/>
    </controller>
    <controller type='pci' index='0' model='pci-root'>
      <alias name='pci.0'/>
    </controller>
    <controller type='scsi' index='0' model='lsilogic'>
      <alias name='scsi0'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x04' function='0x0'/>
    </controller>
    <interface type='network'>
      <mac address='52:54:00:3e:dc:0a'/>
      <source network='mk-minikube' portid='ae2014aa-22b2-4146-bb49-5fc95ac384b8' bridge='virbr1'/>
      <target dev='vnet2'/>
      <model type='virtio'/>
      <alias name='net0'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x0'/>
    </interface>
    <interface type='network'>
      <mac address='52:54:00:4d:e3:a1'/>
      <source network='default' portid='1da6d01b-fea1-4307-9bd5-29d96f448189' bridge='virbr0'/>
      <target dev='vnet3'/>
      <model type='virtio'/>
      <alias name='net1'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>
    <serial type='pty'>
      <source path='/dev/pts/2'/>
      <target type='isa-serial' port='0'>
        <model name='isa-serial'/>
      </target>
      <alias name='serial0'/>
    </serial>
    <console type='pty' tty='/dev/pts/2'>
      <source path='/dev/pts/2'/>
      <target type='serial' port='0'/>
      <alias name='serial0'/>
    </console>
    <input type='mouse' bus='ps2'>
      <alias name='input0'/>
    </input>
    <input type='keyboard' bus='ps2'>
      <alias name='input1'/>
    </input>
    <audio id='1' type='none'/>
    <memballoon model='virtio'>
      <alias name='balloon0'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x06' function='0x0'/>
    </memballoon>
    <rng model='virtio'>
      <backend model='random'>/dev/random</backend>
      <alias name='rng0'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x07' function='0x0'/>
    </rng>
  </devices>
  <seclabel type='dynamic' model='selinux' relabel='yes'>
    <label>system_u:system_r:svirt_t:s0:c227,c890</label>
    <imagelabel>system_u:object_r:svirt_image_t:s0:c227,c890</imagelabel>
  </seclabel>
  <seclabel type='dynamic' model='dac' relabel='yes'>
    <label>+107:+107</label>
    <imagelabel>+107:+107</imagelabel>
  </seclabel>
</domain>
"""

class TestMachineTypeLogic(unittest.TestCase):
    """Tests specifically for the machine type string transformation logic."""

    def _get_xml_with_machine(self, machine_str):
        xml_str = f"<domain><os><type machine='{machine_str}'>hvm</type></os></domain>"
        return ET.fromstring(xml_str)

    def test_version_preservation(self):
        # Case: Standard i440fx with version
        root = self._get_xml_with_machine("pc-i440fx-10.1")
        changed = ensure_machine_type(root)
        self.assertTrue(changed)
        self.assertEqual(root.find("./os/type").get("machine"), "pc-q35-10.1")

    def test_older_version_preservation(self):
         # Case: Older version style
        root = self._get_xml_with_machine("pc-i440fx-2.5")
        ensure_machine_type(root)
        self.assertEqual(root.find("./os/type").get("machine"), "pc-q35-2.5")

    def test_generic_aliases(self):
        # Case: "pc" or "i440fx" -> "q35"
        for alias in ["pc", "i440fx"]:
            root = self._get_xml_with_machine(alias)
            ensure_machine_type(root)
            self.assertEqual(root.find("./os/type").get("machine"), "q35")

    def test_already_q35(self):
        # Case: Already q35 (no change expected)
        root = self._get_xml_with_machine("pc-q35-4.2")
        changed = ensure_machine_type(root)
        self.assertFalse(changed)
        self.assertEqual(root.find("./os/type").get("machine"), "pc-q35-4.2")


class TestHelperFunctions(unittest.TestCase):
    """Specific granular tests for ensure_ioapic, ensure_iommu, and ensure_igb_interface."""

    # --- IOAPIC TESTS ---
    def test_ensure_ioapic_creates_features(self):
        root = ET.fromstring("<domain></domain>")
        modified = ensure_ioapic(root)
        self.assertTrue(modified)
        self.assertEqual(root.find("./features/ioapic").get("driver"), "qemu")

    def test_ensure_ioapic_updates_wrong_driver(self):
        root = ET.fromstring("<domain><features><ioapic driver='kvm'/></features></domain>")
        modified = ensure_ioapic(root)
        self.assertTrue(modified)
        self.assertEqual(root.find("./features/ioapic").get("driver"), "qemu")

    def test_ensure_ioapic_no_change_needed(self):
        root = ET.fromstring("<domain><features><ioapic driver='qemu'/></features></domain>")
        modified = ensure_ioapic(root)
        self.assertFalse(modified)

    # --- IOMMU TESTS ---
    def test_ensure_iommu_creates_devices(self):
        root = ET.fromstring("<domain></domain>")
        modified = ensure_iommu(root)
        self.assertTrue(modified)
        iommu = root.find("./devices/iommu")
        self.assertEqual(iommu.get("model"), "intel")
        self.assertEqual(iommu.find("driver").get("intremap"), "on")

    def test_ensure_iommu_fixes_partial_config(self):
        # Case: intremap is off, iotlb is missing
        xml = "<domain><devices><iommu model='intel'><driver intremap='off'/></iommu></devices></domain>"
        root = ET.fromstring(xml)
        modified = ensure_iommu(root)
        self.assertTrue(modified)
        driver = root.find("./devices/iommu/driver")
        self.assertEqual(driver.get("intremap"), "on")
        self.assertEqual(driver.get("iotlb"), "on")

    # --- IGB INTERFACE TESTS ---
    def test_ensure_igb_injects_new_interface(self):
        """Should create <interface> with model=igb and correct network."""
        root = ET.fromstring("<domain><devices></devices></domain>")
        modified = ensure_igb_interface(root)
        
        self.assertTrue(modified)
        
        # Find interface with model=igb
        igb_iface = None
        for iface in root.findall("./devices/interface"):
            model = iface.find("model")
            if model is not None and model.get("type") == "igb":
                igb_iface = iface
                break
        
        self.assertIsNotNone(igb_iface)
        self.assertEqual(igb_iface.find("source").get("network"), SRIOV_NETWORK_NAME)
        self.assertEqual(igb_iface.get("type"), "network")

    def test_ensure_igb_idempotency_check(self):
        """Should return False if an igb interface already exists."""
        xml = f"""
        <domain><devices>
            <interface type='network'>
               <source network='{SRIOV_NETWORK_NAME}'/>
               <model type='igb'/>
            </interface>
        </devices></domain>
        """
        root = ET.fromstring(xml)
        modified = ensure_igb_interface(root)
        self.assertFalse(modified)
        # Ensure we didn't add a second one
        self.assertEqual(len(root.findall("./devices/interface")), 1)


class TestDramachinePipeline(unittest.TestCase):
    """Tests for the full XML parsing and mutation pipeline using SAMPLE_DOMAIN_XML."""

    def test_minikube_transformations_success(self):
        """
        Verify that a Minikube domain gets all 4 required changes:
        1. machine -> q35
        2. ioapic -> driver='qemu'
        3. iommu -> model='intel'
        4. igb interface -> injected
        """
        input_stream = io.BytesIO(SAMPLE_DOMAIN_XML)
        output_stream = io.BytesIO()

        exit_code = process_xml_stream(input_stream, output_stream)
        self.assertEqual(exit_code, 0)

        output_data = output_stream.getvalue()
        root = ET.fromstring(output_data)

        # 1. Machine
        self.assertEqual(root.find("./os/type").get("machine"), "pc-q35-10.1")
        # 2. IOAPIC
        self.assertEqual(root.find("./features/ioapic").get("driver"), "qemu")
        # 3. IOMMU
        self.assertEqual(root.find("./devices/iommu").get("model"), "intel")
        # 4. IGB
        igb_found = False
        for iface in root.findall("./devices/interface"):
            model = iface.find("model")
            if model is not None and model.get("type") == "igb":
                igb_found = True
                self.assertEqual(iface.find("source").get("network"), SRIOV_NETWORK_NAME)
        self.assertTrue(igb_found, "IGB interface was not injected")

    def test_non_minikube_bypass(self):
        """Verify non-minikube domains are untouched."""
        # Modify name
        modified_xml = SAMPLE_DOMAIN_XML.replace(b"<name>minikube-m02</name>", b"<name>prod-db</name>")
        
        input_stream = io.BytesIO(modified_xml)
        output_stream = io.BytesIO()
        process_xml_stream(input_stream, output_stream)

        # Output should be byte-for-byte identical to input
        self.assertEqual(output_stream.getvalue(), modified_xml)

    def test_malformed_xml(self):
        """Test behavior when input is garbage."""
        input_stream = io.BytesIO(b"<domain><unclosed_tag>")
        output_stream = io.BytesIO()

        # Suppress stderr
        with unittest.mock.patch('sys.stderr', new=io.StringIO()):
            exit_code = process_xml_stream(input_stream, output_stream)

        self.assertEqual(exit_code, 1)
        self.assertEqual(output_stream.getvalue(), b"")

    def test_empty_input(self):
        """Test behavior when input is empty."""
        input_stream = io.BytesIO(b"")
        output_stream = io.BytesIO()
        exit_code = process_xml_stream(input_stream, output_stream)
        self.assertEqual(exit_code, 0)
        self.assertEqual(output_stream.getvalue(), b"")

    def test_full_pipeline_idempotency(self):
        """Run the pipeline twice and ensure the second run changes nothing."""
        # Pass 1
        in1 = io.BytesIO(SAMPLE_DOMAIN_XML)
        out1 = io.BytesIO()
        process_xml_stream(in1, out1)
        pass1_xml = out1.getvalue()

        # Pass 2 (feed output of pass 1)
        in2 = io.BytesIO(pass1_xml)
        out2 = io.BytesIO()
        process_xml_stream(in2, out2)
        pass2_xml = out2.getvalue()

        # They should be identical bytes
        self.assertEqual(pass1_xml, pass2_xml)
        
        # Double check that we don't have duplicates
        root = ET.fromstring(pass2_xml)
        igb_count = 0
        for iface in root.findall("./devices/interface"):
            model = iface.find("model")
            if model is not None and model.get("type") == "igb":
                igb_count += 1
        self.assertEqual(igb_count, 1, "Should not duplicate igb interface on second run")

if __name__ == "__main__":
    import unittest.mock
    unittest.main()
