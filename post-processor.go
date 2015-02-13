package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/mitchellh/packer/common"
	"github.com/mitchellh/packer/packer"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/types"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

var builtins = map[string]string{
	"mitchellh.virtualbox": "virtualbox",
}

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	Datacenter string `mapstructure:"datacenter"`
	Datastore  string `mapstructure:"datastore"`
	Host       string `mapstructure:"host"`
	Password   string `mapstructure:"password"`
	Username   string `mapstructure:"username"`
	VMFolder   string `mapstructure:"vm_folder"`
	VMNetwork  string `mapstructure:"vm_network"`

	tpl *packer.ConfigTemplate
}

type PostProcessor struct {
	config Config
}

func (p *PostProcessor) Configure(raws ...interface{}) error {
	_, err := common.DecodeConfig(&p.config, raws...)
	if err != nil {
		return err
	}

	p.config.tpl, err = packer.NewConfigTemplate()
	if err != nil {
		return err
	}
	p.config.tpl.UserVars = p.config.PackerUserVars

	// Accumulate any errors
	errs := new(packer.MultiError)

	if _, err := exec.LookPath("ovftool"); err != nil {
		errs = packer.MultiErrorAppend(
			errs, fmt.Errorf("ovftool not found: %s", err))
	}

	// First define all our templatable parameters that are _required_
	templates := map[string]*string{
		"datacenter": &p.config.Datacenter,
		"host":       &p.config.Host,
		"password":   &p.config.Password,
		"username":   &p.config.Username,
		"datastore":  &p.config.Datastore,
		"vm_folder":  &p.config.VMFolder,
		"vm_network": &p.config.VMNetwork,
	}
	for key, ptr := range templates {
		if *ptr == "" {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("%s must be set", key))
		}
	}

	// Template process
	for key, ptr := range templates {
		*ptr, err = p.config.tpl.Process(*ptr, nil)
		if err != nil {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("Error processing %s: %s", key, err))
		}
	}

	if len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
	if _, ok := builtins[artifact.BuilderId()]; !ok {
		return nil, false, fmt.Errorf("Unknown artifact type, can't build box: %s", artifact.BuilderId())
	}

	ova := ""
	for _, path := range artifact.Files() {
		if strings.HasSuffix(path, ".ova") {
			ova = path
			break
		}
	}

	if ova == "" {
		return nil, false, fmt.Errorf("OVA not found")
	}

	// Sweet, we've got an OVA, Now it's time to make that baby something we can work with.
	command := exec.Command("ovftool", "--lax", "--allowAllExtraConfig", fmt.Sprintf("--extraConfig:ethernet0.networkName=%s", p.config.VMNetwork), ova, fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova")))

	var ovftoolOut bytes.Buffer
	command.Stdout = &ovftoolOut
	if err := command.Run(); err != nil {
		return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, ovftoolOut.String())
	}

	ui.Message(fmt.Sprintf("%s", ovftoolOut.String()))

	vmdk := fmt.Sprintf("%s-disk1.vmdk", strings.TrimSuffix(ova, ".ova"))
	vmx := fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))

	ui.Message("Replacing the hardware version in the vmx")

	vmxContent, err := ioutil.ReadFile(vmx)
	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}
	lines := strings.Split(string(vmxContent), "\n")
	for i, line := range lines {
		if strings.Contains(line, "virtualhw.version") {
			lines[i] = "virtualhw.version = \"10\""
		}
	}
	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(vmx, []byte(output), 0644)
	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	ui.Message(fmt.Sprintf("Now going to upload %s and %s to Datastore %s on host %s", vmdk, vmx, p.config.Datastore, p.config.Host))

	err = doUpload(fmt.Sprintf("https://%s:%s@%s/folder/%s/%s?dcPath=%s&dsName=%s",
		url.QueryEscape(p.config.Username),
		url.QueryEscape(p.config.Password),
		p.config.Host,
		p.config.VMFolder,
		vmdk,
		p.config.Datacenter,
		p.config.Datastore), vmdk)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	ui.Message(fmt.Sprintf("Uploaded %s", vmdk))

	err = doUpload(fmt.Sprintf("https://%s:%s@%s/folder/%s/%s?dcPath=%s&dsName=%s",
		url.QueryEscape(p.config.Username),
		url.QueryEscape(p.config.Password),
		p.config.Host,
		p.config.VMFolder,
		vmx,
		p.config.Datacenter,
		p.config.Datastore), vmx)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	ui.Message(fmt.Sprintf("Uploaded %s", vmx))

	err = doRegistration(ui, p.config, vmx)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}
	ui.Message("Uploaded and registered to VMware")

	return artifact, false, nil
}

func doUpload(url string, file string) (err error) {

	data, err := os.Open(file)
	if err != nil {
		return err
	}
	defer data.Close()

	fileInfo, err := data.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, data)

	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = fileInfo.Size()
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	return nil
}

func doRegistration(ui packer.Ui, config Config, vmx string) (err error) {

	sdkURL, err := url.Parse(fmt.Sprintf("https://%s:%s@%s/sdk",
		url.QueryEscape(config.Username),
		url.QueryEscape(config.Password),
		config.Host))
	if err != nil {
		return err
	}

	client, err := govmomi.NewClient(*sdkURL, true)

	if err != nil {
		return err
	}

	finder := find.NewFinder(client, false)
	datacenter, err := finder.DefaultDatacenter()
	finder.SetDatacenter(datacenter)
	if err != nil {
		return err
	}

	folders, err := datacenter.Folders()
	if err != nil {
		return err
	}

	resourcePool, err := finder.DefaultResourcePool()

	if err != nil {
		return err
	}

	datastoreString := fmt.Sprintf("[%s] %s/%s.vmx", config.Datastore, config.VMFolder, strings.TrimSuffix(vmx, ".vmx"))
	splitString := strings.Split(vmx, "/")
	last := splitString[len(splitString)-1]
	vmName := strings.TrimSuffix(last, ".vmx")

	ui.Message(fmt.Sprintf("Registering %s from %s", vmName, datastoreString))
	task, err := folders.VmFolder.RegisterVM(datastoreString, vmName, false, resourcePool, nil)
	if err != nil {
		return err
	}
	_, err = task.WaitForResult(nil)
	if err != nil {
		return err
	}
	ui.Message(fmt.Sprintf("Registererd VM %s", vmName))

	vm, err := finder.VirtualMachine(vmName)

	rpRef := resourcePool.Reference()

	cloneSpec := types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			Pool: &rpRef,
		},
	}

	cloneVmName := fmt.Sprintf("%s-vm", vmName)

	ui.Message(fmt.Sprintf("Cloning VM %s", cloneVmName))
	task, err = vm.Clone(folders.VmFolder, cloneVmName, cloneSpec)

	if err != nil {
		return err
	}

	_, err = task.WaitForResult(nil)

	if err != nil {
		return err
	}

	clonedVM, err := finder.VirtualMachine(cloneVmName)

	if err != nil {
		return err
	}

	ui.Message(fmt.Sprintf("Powering on %s", cloneVmName))
	task, err = clonedVM.PowerOn()

	if err != nil {
		return err
	}

	_, err = task.WaitForResult(nil)
	if err != nil {
		return err
	}

	ui.Message(fmt.Sprintf("Powered on %s", cloneVmName))

	time.Sleep(150000 * time.Millisecond) // This is really dirty, but I need to make sure the VM gets fully powered on before I turn it off, otherwise vmware tools won't register on the cloning side.

	ui.Message(fmt.Sprintf("Powering off %s", cloneVmName))
	task, err = clonedVM.PowerOff()

	if err != nil {
		return err
	}

	_, err = task.WaitForResult(nil)

	if err != nil {
		return err
	}
	ui.Message(fmt.Sprintf("Powered off %s", cloneVmName))

	ui.Message(fmt.Sprintf("Marking as template %s", cloneVmName))
	err = clonedVM.MarkAsTemplate()

	if err != nil {
		return err
	}

	ui.Message(fmt.Sprintf("Destroying %s", cloneVmName))
	task, err = vm.Destroy()

	_, err = task.WaitForResult(nil)

	if err != nil {
		return err
	}
	ui.Message(fmt.Sprintf("Destroyed %s", cloneVmName))

	return nil

}
