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


Make sure that the directory which contains the packer-post-processor-vsphere-ova executable is your PATH environmental variable (see http://www.packer.io/docs/extend/plugins.html -> Installing Plugins)

## Usage
Add the following, filled out correctly to your post-processors and you should end up with `packer-virutalbox-timestamp-vm` registered on your cluster as a template.

1. It uploads and registers the virtual maching using 'ovftool' in the 'Templates' folder
1. It marks the cloned virtual machine as a template.

This is the statement you need to add to your packer json file:

```
"post-processors": [
    {
      "type": "vsphere-ova",
      "host":"vcenter_host",
      "datacenter":"datacenter_name",
      "cluster":"cluster",
      "username":"my_username",
      "password":"my_password",
      "datastore": "datastore_name",
    }
]
```

You also will need ```"format": "ova"``` in your virtualbox-iso builder isection of your packer template, this is not required if using VMware builders. (see: http://www.packer.io/docs/other/core-configuration.html -> Core Configuration)

NOTE: This will produce the default behavior described above, you can avoid steps 3-6 if you remove the Floppy, Optical Drive, and Ethernet devices prior to upload.  See below for how to do this.

### Specifying an alternate folder to hold the Template

Add ```"vm_folder":"folder_name"``` to the post-processor config in your packer template.  'folder_name' is realative to the Datacenter name.  Default: "Templates"

### Specifying a specific virtual network to connect to

Add ```"vm_network":"vmware_network_name"``` to the post-processor config in your packer template.  'vmware_network_name' Default: "VM Network"

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

