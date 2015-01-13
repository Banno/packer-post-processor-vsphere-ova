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
Add the following, filled out correctly to your post-processors and you should end up with folder_on_datastore/output-virtualbox-iso/packer-virtualbox-iso-{{timestamp}}-disk1.vmdk and packer-virtualbox-iso-{{timestamp}}.vmtx on your datastore. From there you can register and deploy from template on the vmtx. You may have to customize the hardware when deploying so that you can assign a network to the NIC.


```
"post-processors": [
    {
      "type": "vsphere-ova",
      "host":"vcenter_host",
      "datacenter":"datacenter_name",
      "username":"my_username",
      "password":"my_password",
      "vm_name": "packer_template",
      "datastore": "datastore_name",
      "vm_folder":"folder_on_datastore"
    }
]
```
