package main

import (
	"github.com/banno/packer-post-processor-vsphere-ova/vsphereova"
	"github.com/hashicorp/packer/packer/plugin"
)

func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterPostProcessor(new(vsphereova.PostProcessor))
	server.Serve()
}
