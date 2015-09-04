# packer-post-processor-vsphere-ova

This post-processor will upload vmware template to a datastore through VSphere 5.5 and/or optionally create an ova file locally.

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

Pull the repository and compile the code with ```go build``` then copy it to your Packer install directory.  You can also just type ```go install .``` if your Packer binaries are in '$GOPATH/bin'.


Make sure that the directory which contains the packer-post-processor-vsphere-ova executable is your PATH environmental variable (see http://www.packer.io/docs/extend/plugins.html -> Installing Plugins)

## Usage

NOTE: For Virtualbox builders only, you also will need ```"format": "ova"``` in your virtualbox-iso builder section of your packer template.

### Make a vSphere Template - The default behavior.
Add the following, filled out correctly to your post-processors and you should end up with a new template registered on your cluster and an ova file in ./ova/[builder_type].

1. It uploads and registers the virtual machine using 'ovftool' in the 'Templates' folder
1. It marks the uploaded virtual machine as a template.

Add to your packer json file:

```
"post-processors": [
    {
      "type": "vsphere-ova",
      "host":"vcenter_host",
      "datacenter":"datacenter_name",
      "cluster":"cluster",
      "username":"my_username",
      "password":"my_password",
      "datastore": "datastore_name"
    }
]
```

### Make a Local OVA File
Add the following, filled out correctly to your post-processors and you should end up with an ova file in ./ova/[builder_type].

1. Export an OVA file in ./ova/[builder_type].

Add to your packer json file:

```
"post-processors": [
    {
      "type": "vsphere-ova",
      "output_artifact_type:" "ova"
    }
]
```

### Make a both vSphere Template and a Local OVA file
Add the following, filled out correctly to your post-processors and you should end up with a new template registered on your cluster.

1. Uploads and registers the virtual maching using 'ovftool' in the 'Templates' folder
1. Marks the uploaded virtual machine as a template.
1. Export an OVA file in ./ova/[builder_type].

Add to your packer json file:

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
      "output_artifact_type:" "ova_template"
    }
]
```

### Specifying the output artifact type

Add ```"output_artifact_type":"ova|template|ova_template"``` to the post-processor config in your packer template.  'output_artifact_type' Default: "template"

* ova            Produces an OVA file in ./ova/[builder_type].
* template       Uploads a template to the specified vSphere.
* ova_template   Produces an OVA file in ./ova/[builder_type] and uploads a template to the specified vSphere.

### Specifying an alternate folder to hold the Template

Add ```"vm_folder":"folder_name"``` to the post-processor config in your packer template.  'folder_name' is realative to the Datacenter name.  Default: "Templates"

### Specifying a specific virtual network to connect to

Add ```"vm_network":"vmware_network_name"``` to the post-processor config in your packer template.  'vmware_network_name' Default: "VM Network"

### Specifying a Virtual Hardware Version Before Uploading to Vsphere

Add ```"virtual_hardware_version": "n"``` to the post-processor config in your packer template. Where 'n' is the desired version.  Default: 10

### Removing the Floppy Before Uploading to Vsphere

Add ```"remove_floppy": "true"``` to the post-processor config in your packer template.

### Removing the Ethernet0 Interface Before Uploading to Vsphere
NOTE: Do not use with 'vm_network'.

Add ```"remove_ethernet": "true"``` to the post-processor config in your packer template.  

### Removing the Optical Drive Before Uploading to Vsphere

Add ```"remove_optical_drive": "true"``` to the post-processor config in your packer template.
