package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cheggaaa/pb"
	vmwarecommon "github.com/hashicorp/packer/builder/vmware/common"
	"github.com/hashicorp/packer/common"
	"github.com/hashicorp/packer/helper/config"
	"github.com/hashicorp/packer/packer"
	"github.com/hashicorp/packer/template/interpolate"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

var builtins = map[string]string{
	"mitchellh.virtualbox": "virtualbox",
	"mitchellh.vmware":     "vmware",
}

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	Datacenter         string `mapstructure:"datacenter"`
	Datastore          string `mapstructure:"datastore"`
	Host               string `mapstructure:"host"`
	Password           string `mapstructure:"password"`
	Username           string `mapstructure:"username"`
	VMFolder           string `mapstructure:"vm_folder"`
	VMNetwork          string `mapstructure:"vm_network"`
	RemoveEthernet     string `mapstructure:"remove_ethernet"`
	RemoveFloppy       string `mapstructure:"remove_floppy"`
	RemoveOpticalDrive string `mapstructure:"remove_optical_drive"`
	VirtualHardwareVer string `mapstructure:"virtual_hardware_version"`
	ResourcePool       string `mapstructure:"resource_pool"`
	VMWareGuestOsType  string `mapstructure:"vm_guest_os_type"`
	ctx                interpolate.Context
}

type PostProcessor struct {
	config Config
}

func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		Interpolate: true,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)

	if err != nil {
		return err
	}

	// Defaults
	if p.config.RemoveEthernet == "" {
		p.config.RemoveEthernet = "false"
	}

	if p.config.RemoveFloppy == "" {
		p.config.RemoveFloppy = "false"
	}

	if p.config.RemoveOpticalDrive == "" {
		p.config.RemoveOpticalDrive = "false"
	}

	if p.config.VirtualHardwareVer == "" {
		p.config.VirtualHardwareVer = "10"
	}

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
	}

	for key, ptr := range templates {
		if *ptr == "" {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("%s must be set", key))
		}
	}

	if len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

func (p *PostProcessor) RemoveFloppy(vmx string, ui packer.Ui) error {
	ui.Message(fmt.Sprintf("Removing floppy from %s", vmx))
	vmxData, err := vmwarecommon.ReadVMX(vmx)
	if err != nil {
		return err
	}
	for k, _ := range vmxData {
		if strings.HasPrefix(k, "floppy0.") {
			delete(vmxData, k)
		}
	}
	vmxData["floppy0.present"] = "FALSE"
	if err := vmwarecommon.WriteVMX(vmx, vmxData); err != nil {
		return err
	}
	return nil
}

func (p *PostProcessor) RemoveEthernet(vmx string, ui packer.Ui) error {
	ui.Message(fmt.Sprintf("Removing ethernet0 interface from %s", vmx))
	vmxData, err := vmwarecommon.ReadVMX(vmx)
	if err != nil {
		return err
	}

	for k, _ := range vmxData {
		if strings.HasPrefix(k, "ethernet0.") {
			delete(vmxData, k)
		}
	}

	vmxData["ethernet0.present"] = "FALSE"
	if err := vmwarecommon.WriteVMX(vmx, vmxData); err != nil {
		return err
	}

	return nil
}

func (p *PostProcessor) SetVHardwareVersion(vmx string, ui packer.Ui, hwversion string) error {
	ui.Message(fmt.Sprintf("Setting the hardware version in the vmx to version '%s'", hwversion))

	vmxContent, err := ioutil.ReadFile(vmx)
	lines := strings.Split(string(vmxContent), "\n")
	for i, line := range lines {
		if strings.Contains(line, "virtualhw.version") {
			lines[i] = fmt.Sprintf("virtualhw.version = \"%s\"", hwversion)
		}
	}
	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(vmx, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (p *PostProcessor) SetGuestOs(vmx string, ui packer.Ui, os string) error {
	vmxContent, err := ioutil.ReadFile(vmx)
	lines := strings.Split(string(vmxContent), "\n")
	updated := false
	guestOsLine := fmt.Sprintf("guestos = \"%s\"", os)
	for i, line := range lines {
		if strings.Contains(line, "guestos") {
			lines[i] = guestOsLine
			ui.Message(fmt.Sprintf("Updated the OS type in the vmx to '%s'", os))
			updated = true
		}
	}
	if updated == false {
		lines = append(lines, guestOsLine)
		ui.Message(fmt.Sprintf("Append the OS type in the vmx to '%s'", os))
	}
	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(vmx, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (p *PostProcessor) RemoveOpticalDrive(vmx string, ui packer.Ui) error {
	ui.Message(fmt.Sprintf("Removing optical drive from %s", vmx))
	vmxData, err := vmwarecommon.ReadVMX(vmx)
	if err != nil {
		return err
	}

	for k, _ := range vmxData {
		if strings.HasPrefix(k, "ide1:0.file") {
			delete(vmxData, k)
		}
	}

	vmxData["ide1:0.present"] = "FALSE"

	if err := vmwarecommon.WriteVMX(vmx, vmxData); err != nil {
		return err
	}
	return nil
}

func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
	if _, ok := builtins[artifact.BuilderId()]; !ok {
		return nil, false, fmt.Errorf("Unknown artifact type, can't build box: %s", artifact.BuilderId())
	}

	ova := ""
	vmx := ""
	vmdk := ""
	for _, path := range artifact.Files() {
		if strings.HasSuffix(path, ".ova") {
			ova = path
			break
		} else if strings.HasSuffix(path, ".vmx") {
			vmx = path
		} else if strings.HasSuffix(path, ".vmdk") {
			vmdk = path
		}
	}

	if ova == "" && (vmx == "" || vmdk == "") {
		return nil, false, fmt.Errorf("ERROR: Neither OVA or VMX/VMDK were found!")
	}

	if ova != "" {
		// Sweet, we've got an OVA, Now it's time to make that baby something we can work with.
		var args []string
		//if p.config.RemoveEthernet != "true" {
		//	args = append(args,
		//		"--allowExtraConfig",
		//		fmt.Sprintf("--extraConfig:ethernet0.networkName=%s", p.config.VMNetwork),
		//	)
		//}
		args = append(args,
			"--lax",
			ova,
			fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova")),
		)

		command := exec.Command("ovftool", args...)
		var ovftoolOut bytes.Buffer
		command.Stdout = &ovftoolOut
		if err := command.Run(); err != nil {
			return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, ovftoolOut.String())
		}

		ui.Message(fmt.Sprintf("%s", ovftoolOut.String()))

		vmdk = fmt.Sprintf("%s-disk1.vmdk", strings.TrimSuffix(ova, ".ova"))
		vmx = fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))
	}

	if p.config.RemoveEthernet == "true" {
		if err := p.RemoveEthernet(vmx, ui); err != nil {
			return nil, false, fmt.Errorf("Removing ethernet0 interface from VMX failed!")
		}
	}

	if p.config.RemoveFloppy == "true" {
		if err := p.RemoveFloppy(vmx, ui); err != nil {
			return nil, false, fmt.Errorf("Removing floppy drive from VMX failed!")
		}
	}

	if p.config.RemoveOpticalDrive == "true" {
		if err := p.RemoveOpticalDrive(vmx, ui); err != nil {
			return nil, false, fmt.Errorf("Removing CD/DVD Drive from VMX failed!")
		}
	}

	if p.config.VirtualHardwareVer != "" {
		if err := p.SetVHardwareVersion(vmx, ui, p.config.VirtualHardwareVer); err != nil {
			return nil, false, fmt.Errorf("Setting the Virtual Hardware Version in VMX failed!")
		}
	}

	if p.config.VMWareGuestOsType != "" {
		if err := p.SetGuestOs(vmx, ui, p.config.VMWareGuestOsType); err != nil {
			return nil, false, fmt.Errorf("Setting the Guest OS in VMX failed!")
		}
	}

	ui.Message(fmt.Sprintf("Uploading %s and %s to Datastore %s on host %s", vmdk, vmx, p.config.Datastore, p.config.Host))

	clonerequired := false
	if p.config.RemoveEthernet == "false" || p.config.RemoveFloppy == "false" || p.config.RemoveOpticalDrive == "false" {
		clonerequired = true
	}

	splitString := strings.Split(vmdk, "/")
	vmdkDestPath := fmt.Sprintf("folder/%s/%s", p.config.VMFolder, splitString[len(splitString)-1])

	splitString = strings.Split(vmx, "/")
	vmxDestPath := fmt.Sprintf("folder/%s/%s", p.config.VMFolder, splitString[len(splitString)-1])

	err := doUpload(
		ui,
		fmt.Sprintf("https://%s:%s@%s/%s?dcPath=%s&dsName=%s",
			url.QueryEscape(p.config.Username),
			url.QueryEscape(p.config.Password),
			p.config.Host,
			vmdkDestPath,
			p.config.Datacenter,
			p.config.Datastore),
		vmdk)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	ui.Message(fmt.Sprintf("Uploaded %s", vmdk))

	err = doUpload(
		ui,
		fmt.Sprintf("https://%s:%s@%s/%s?dcPath=%s&dsName=%s",
			url.QueryEscape(p.config.Username),
			url.QueryEscape(p.config.Password),
			p.config.Host,
			vmxDestPath,
			p.config.Datacenter,
			p.config.Datastore),
		vmx)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	ui.Message(fmt.Sprintf("Uploaded %s", vmx))

	err = doRegistration(ui, p.config, vmx, clonerequired)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}
	ui.Message("Uploaded and registered to VMware")

	return artifact, false, nil
}

func doUpload(ui packer.Ui, url string, file string) error {

	data, err := os.Open(file)
	if err != nil {
		return err
	}
	defer data.Close()

	fileInfo, err := data.Stat()
	if err != nil {
		return err
	}

	bar := pb.New64(fileInfo.Size()).SetUnits(pb.U_BYTES)
	bar.ShowSpeed = true
	bar.Callback = ui.Message
	bar.RefreshRate = time.Second * 5
	bar.SetWidth(40)
	reader := bar.NewProxyReader(data)

	req, err := http.NewRequest("PUT", url, reader)
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = fileInfo.Size()

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}

	bar.Start()
	res, err := client.Do(req)
	bar.Finish()

	if err != nil {
		return err
	}

	defer res.Body.Close()

	return nil
}

func doRegistration(ui packer.Ui, config Config, vmx string, clonerequired bool) error {

	sdkURL, err := url.Parse(fmt.Sprintf("https://%s:%s@%s/sdk",
		url.QueryEscape(config.Username),
		url.QueryEscape(config.Password),
		config.Host))
	if err != nil {
		return err
	}

	client, err := govmomi.NewClient(context.TODO(), sdkURL, true)

	if err != nil {
		return err
	}

	finder := find.NewFinder(client.Client, false)
	datacenter, err := finder.DatacenterOrDefault(context.TODO(), config.Datacenter)
	if err != nil {
		return err
	}
	finder.SetDatacenter(datacenter)

	folders, err := datacenter.Folders(context.TODO())
	if err != nil {
		return err
	}

	resourcePool, err := finder.ResourcePoolOrDefault(context.TODO(), config.ResourcePool)

	if err != nil {
		return err
	}

	splitString := strings.Split(vmx, "/")
	last := splitString[len(splitString)-1]
	vmName := strings.TrimSuffix(last, ".vmx")

	datastoreString := fmt.Sprintf("[%s] %s/%s.vmx", config.Datastore, config.VMFolder, vmName)

	ui.Message(fmt.Sprintf("Registering %s from %s", vmName, datastoreString))
	task, err := folders.VmFolder.RegisterVM(context.TODO(), datastoreString, vmName, false, resourcePool, nil)
	if err != nil {
		return err
	}
	_, err = task.WaitForResult(context.TODO(), nil)
	if err != nil {
		return err
	}
	ui.Message(fmt.Sprintf("Registererd VM %s", vmName))

	vm, err := finder.VirtualMachine(context.TODO(), vmName)

	rpRef := resourcePool.Reference()

	if clonerequired {
		cloneSpec := types.VirtualMachineCloneSpec{
			Location: types.VirtualMachineRelocateSpec{
				Pool: &rpRef,
			},
		}

		cloneVmName := fmt.Sprintf("%s-vm", vmName)

		ui.Message(fmt.Sprintf("Cloning VM %s", cloneVmName))
		task, err = vm.Clone(context.TODO(), folders.VmFolder, cloneVmName, cloneSpec)

		if err != nil {
			return err
		}

		_, err = task.WaitForResult(context.TODO(), nil)

		if err != nil {
			return err
		}

		clonedVM, err := finder.VirtualMachine(context.TODO(), cloneVmName)

		if err != nil {
			return err
		}

		ui.Message(fmt.Sprintf("Powering on %s", cloneVmName))
		task, err = clonedVM.PowerOn(context.TODO())

		if err != nil {
			return err
		}

		_, err = task.WaitForResult(context.TODO(), nil)
		if err != nil {
			return err
		}

		ui.Message(fmt.Sprintf("Powered on %s", cloneVmName))

		timeout := time.After(5 * time.Minute)
		tick := time.Tick(500 * time.Millisecond)

	LoopWaitForVMToolsRunning:
		for {
			select {
			case <-timeout:
				task, err = clonedVM.PowerOff(context.TODO())
				if err != nil {
					return err
				}
				_, err = task.WaitForResult(context.TODO(), nil)
				if err != nil {
					return err
				}
				return fmt.Errorf("Timed out while waiting for VM Tools to be recogonized")
			case <-tick:
				running, err := clonedVM.IsToolsRunning(context.TODO())
				if err != nil {
					return err
				}
				if running {
					break LoopWaitForVMToolsRunning
				}
			}
		}

		ui.Message(fmt.Sprintf("Powering off %s", cloneVmName))
		task, err = clonedVM.PowerOff(context.TODO())

		if err != nil {
			return err
		}

		_, err = task.WaitForResult(context.TODO(), nil)

		if err != nil {
			return err
		}
		ui.Message(fmt.Sprintf("Powered off %s", cloneVmName))

		ui.Message(fmt.Sprintf("Marking as template %s", cloneVmName))
		err = clonedVM.MarkAsTemplate(context.TODO())

		if err != nil {
			return err
		}

		ui.Message(fmt.Sprintf("Destroying %s", cloneVmName))
		task, err = vm.Destroy(context.TODO())

		_, err = task.WaitForResult(context.TODO(), nil)

		if err != nil {
			return err
		}
		ui.Message(fmt.Sprintf("Destroyed %s", cloneVmName))
	} else {
		ui.Message(fmt.Sprintf("Marking as template %s", vmName))
		err = vm.MarkAsTemplate(context.TODO())

		if err != nil {
			return err
		}
		ui.Message(fmt.Sprintf("%s is now a template", vmName))
	}

	return nil
}

