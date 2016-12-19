# packer-post-processor-vsphere-ova

This post-processor will upload a VMDK and vmware template to a datastore through VSphere 5.5

## Prerequisites

Software:

  * A Local hypervisor for Packer to use during the build:
    1. Oracle [Virtualbox](https://www.virtualbox.org/wiki/Downloads)
    1. VMware [Workstation for Windows/Linux](http://www.vmware.com/products/workstation/workstation-evaluation)/[Player for Windows/Linux](http://www.vmware.com/products/player/playerpro-evaluation.html)/[Fusion for MacOS](https://www.vmware.com/products/fusion/fusion-evaluation)
  * VMware OVF Tool (4.1.0) [Download from VMWare](https://my.vmware.com/web/vmware/details?productId=491&downloadGroup=OVFTOOL410_OSS)
  * vSphere 5.5 (will likely work on 6, but untested)

Notes:

  * This post processor should work with the Virtualbox and the VMware Packer builders

## Installation

Pull the repository and compile the code with ```go build```

Add

```
{
  "post-processors": {
    "vsphere-ova": "packer-post-processor-vsphere-ova"
  }
}
```

to your packer configuration (see: http://www.packer.io/docs/other/core-configuration.html -> Core Configuration)

Make sure that the directory which contains the packer-post-processor-vsphere-ova executable is your PATH environmental variable (see http://www.packer.io/docs/extend/plugins.html -> Installing Plugins)

## Usage
Add the following, filled out correctly to your post-processors and you should end up with `packer-virutalbox-timestamp-vm` registered on your cluster as a template.

I'm not sure if a release of Packer with SCSI support has been released yet, but you can create a virtualbox with a SCSI drive using Packer for maximum performance on your VMWare setup.

There is some wierdness with how this works:

1. It uploads a virtual machine
2. It registers a virtual machine
3. It clones the virtual machine (it complains about invalid device backing
   without this)
4. It powers on the cloned virtual machine
5. It SLEEPS for 2ish minutes while we wait for power on to complete
6. It powers off the cloned virtual machine
7. It marks the cloned virtual machine as a template.
8. You end up with a registered template of the vm name with "-vm" appended.

This is the statement you need to add to your packer json file:

```
"post-processors": [
    {
      "type": "vsphere-ova",
      "host":"vcenter_host",
      "datacenter":"datacenter_name",
      "cluster": "cluster_name |optional",
      "resource_pool": "resource_pool_name |optional",
      "username":"my_username",
      "password":"my_password",
      "datastore": "datastore_name",
      "vm_folder":"folder_on_datastore",
      "vm_network":"vmware_network_name"
    }
]
```

You also will need ```"format": "ova"``` in your virtualbox-iso builder for this to function.

NOTE: This will produce the default behavior described above, you can avoid steps 3-6 if you remove the Floppy, Optical Drive, and Ethernet devices prior to upload.  See below for how to do this.

### Specifying a Virtual Hardware Version Before Uploading to Vsphere

Add ```"virtual_hardware_version": "n"``` to the post-processor config in your packer template. Where 'n' is the desired version.  Default: 10

### Removing the Floppy Before Uploading to Vsphere

Add ```"remove_floppy": "true"``` to the post-processor config in your packer template.

### Removing the Ethernet0 Interface Before Uploading to Vsphere

Add ```"remove_ethernet": "true"``` to the post-processor config in your packer template.

### Removing the Optical Drive Before Uploading to Vsphere

Add ```"remove_optical_drive": "true"``` to the post-processor config in your packer template.

### Avoiding Post-Processing Steps 3-6
Add ```"remove_floppy": "true", "remove_ethernet": "true", "remove_optical_drive": "true"``` to the post-processor config in your packer template.

NOTE: This makes the ```"vm_network": "vmware_network_name"``` parameter optional.
