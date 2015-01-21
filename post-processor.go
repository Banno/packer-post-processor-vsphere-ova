package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/mitchellh/packer/common"
	"github.com/mitchellh/packer/packer"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
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
	ESXHost    string `mapstructure:"esx_host"`
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
		"esx_host":   &p.config.ESXHost,
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
	command := exec.Command("ovftool", "--lax", ova, fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova")))

	var ovftool_out bytes.Buffer
	command.Stdout = &ovftool_out
	if err := command.Run(); err != nil {
		return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, ovftool_out.String())
	}

	ui.Message(fmt.Sprintf("%s", ovftool_out.String()))

	vmdk := fmt.Sprintf("%s-disk1.vmdk", strings.TrimSuffix(ova, ".ova"))
	vmx := fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))

	ui.Message(fmt.Sprintf("Now going to upload %s and %s to Datastore %s on host %s", vmdk, vmx, p.config.Datastore, p.config.Host))

	err := doUpload(fmt.Sprintf("https://%s:%s@%s/folder/%s/%s?dcPath=%s&dsName=%s",
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

	err = doUpload(fmt.Sprintf("https://%s:%s@%s/folder/%s/%s?dcPath=%s&dsName=%s",
		url.QueryEscape(p.config.Username),
		url.QueryEscape(p.config.Password),
		p.config.Host,
		p.config.VMFolder,
		fmt.Sprintf("%s.vmtx", strings.TrimSuffix(vmx, ".vmx")),
		p.config.Datacenter,
		p.config.Datastore), vmx)

	if err != nil {
		return nil, false, fmt.Errorf("Failed: %s", err)
	}

	err = doRegistration(p.config, vmx)

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

	file_info, err := data.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, data)

	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = file_info.Size()
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	request_dump, _ := httputil.DumpRequest(req, false)
	fmt.Print(string(request_dump))

	client := &http.Client{Transport: tr}
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	body, err := httputil.DumpResponse(res, false)
	if err != nil {
		return err
	}

	fmt.Print(string(body))

	defer res.Body.Close()

	return nil
}

func doRegistration(config Config, vmx string) (err error) {

	sdk_url, err := url.Parse(fmt.Sprintf("https://%s:%s@%s/sdk",
		url.QueryEscape(config.Username),
		url.QueryEscape(config.Password),
		config.Host))
	if err != nil {
		return err
	}

	client, err := govmomi.NewClient(*sdk_url, true)

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

	searchIndex := client.SearchIndex()
	shost, err := searchIndex.FindByDnsName(datacenter, config.ESXHost, false)
	if err != nil {
		return err
	}
	datastore_string := fmt.Sprintf("[%s] %s/%s.vmtx", config.Datastore, config.VMFolder, strings.TrimSuffix(vmx, ".vmx"))
	split_string := strings.Split(vmx, "/")
	last := split_string[len(split_string)-1]
	vm_name := strings.TrimSuffix(last, ".vmx")

	task, err := folders.VmFolder.RegisterVM(datastore_string, vm_name, true, nil, shost.(*govmomi.HostSystem))
	if err != nil {
		return err
	}
	info, err := task.WaitForResult(nil)
	if err != nil {
		return err
	}

	if info.State == "success" {
		return nil
	} else {
		return errors.New("Error registering template")
	}

}
