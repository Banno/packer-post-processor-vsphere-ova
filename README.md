# packer-post-processor-vsphere-ova

This post-processor will upload a VMDK and vmware template to a datastore on VSphere 5.5

## Prerequisites

Software:

  * VMware OVF Tool
  
Notes:

  * This post processor only works with the Virtualbox builder

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


```
"post-processors": [
    {
      "type": "vsphere-ova",
      "host":"vcenter_host",
      "datacenter":"datacenter_name",
      "username":"my_username",
      "password":"my_password",
      "datastore": "datastore_name",
      "vm_folder":"folder_on_datastore",
      "vm_network":"vmware_network_name"
    }
]
```
