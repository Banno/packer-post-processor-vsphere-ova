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
  "golang.org/x/net/context"
  "io/ioutil"
  "net/http"
  "net/url"
  "os"
  "os/exec"
  "strings"
  "time"
  "log"
  "path/filepath"
  "github.com/mitchellh/packer/helper/config"
  "github.com/mitchellh/packer/template/interpolate"
  vmwarecommon "github.com/mitchellh/packer/builder/vmware/common"
)

var builtins = map[string]string{
  "mitchellh.virtualbox": "virtualbox",
  "mitchellh.vmware": "vmware",
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
  DiskMode           string `mapstructure:"disk_mode"`
  UploadCommand      string `mapstructure:"upload_command"`
  UploadArgs         string `mapstructure:"upload_args"`
  Compression        uint   `mapstructure:"compression"`
  ctx interpolate.Context
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
  if p.config.VMNetwork == "" {
    p.config.VMNetwork = "VM Network"
  }

  if p.config.DiskMode == "" {
    p.config.DiskMode = "thin"
  }

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

  if !(p.config.Compression >= 0 && p.config.Compression <= 9) {
    errs = packer.MultiErrorAppend(
    errs, fmt.Errorf("Invalid compression level. Must be between 1 and 9, or 0 for no compression."))
  }

  if !(p.config.DiskMode == "thick" ||
    p.config.DiskMode == "thin" ||
    p.config.DiskMode == "monolithicSparse" ||
    p.config.DiskMode == "monolithicFlat" ||
    p.config.DiskMode == "twoGbMaxExtentSparse" ||
    p.config.DiskMode == "twoGbMaxExtentFlat" ||
    p.config.DiskMode == "seSparse" ||
    p.config.DiskMode == "eagerZeroedThick" ||
    p.config.DiskMode == "sparse" ||
    p.config.DiskMode == "flat") {
    errs = packer.MultiErrorAppend(
      errs, fmt.Errorf("Invalid disk_mode. Only thin(Default), thick, monolithicSparse, monolithicFlat, twoGbMaxExtentSparse, twoGbMaxExtentFlat, seSparse, eagerZeroedThick, sparse, and flat are allowed."))
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
  ui.Message(fmt.Sprintf("Removing ethernet0 intercace from %s", vmx))
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

func visit(path string, f os.FileInfo, err error) error {
  fmt.Printf("Visited: %s\n", path)
  return nil
}

func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
  if _, ok := builtins[artifact.BuilderId()]; !ok {
    return nil, false, fmt.Errorf("Unknown artifact type, can't build box: %s", artifact.BuilderId())
  }

  ova  := ""
  vmx  := ""
  vmdk := ""
  for _, path := range artifact.Files() {
    if strings.HasSuffix(path, ".ova") {
      ova = path
      break
    } else if strings.HasSuffix(path, ".vmx") {
      vmx = path
    }
  }

  if ova == "" && vmx == "" {
    return nil, false, fmt.Errorf("ERROR: Neither OVA nor VMX were found!")
  }

  // Convert vmware builder artifact to ova so we can specify the hard disk
  // type best for use for out purposes.
  if artifact.BuilderId() == "mitchellh.vmware" {
    if _, err := os.Stat("ova/vmware"); os.IsNotExist(err) {
      os.Mkdir("ova/vmware",0755)
    }

    splitString := strings.Split(vmx, "/")
    ova = fmt.Sprintf("ova/vmware/%s.ova", strings.TrimSuffix(splitString[len(splitString)-1], ".vmx"))

    args := []string{
      "--acceptAllEulas",
      "-tt=OVA",
      fmt.Sprintf("--diskMode=%s", p.config.DiskMode),
      fmt.Sprintf("%s", vmx),
      fmt.Sprintf("%s", ova),
    }

    ui.Message(fmt.Sprintf("Exporting %s to %s", vmx, ova))
    var out bytes.Buffer
    command := exec.Command("ovftool", args...)
    log.Printf("Starting ovftool with parameters: %s", strings.Join(args, " "))
    command.Stdout = &out
    if err := command.Run(); err != nil {
      return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, out.String())
    }

    // if err := os.Remove(vmx) ; err != nil {
    //   fmt.Println(err)
    //   return nil, false, fmt.Errorf("Failed: Deleting %s", err, vmx)
    // }

    // if err := os.Remove(vmdk) ; err != nil {
    //   fmt.Println(err)
    //   return nil, false, fmt.Errorf("Failed: Deleting %s", err, vmdk)
    // }

    ui.Message(fmt.Sprintf("Conversion of VMX to OVA: %s", out.String()))
  }

  if ova == "" {
    return nil, false, fmt.Errorf("OVA not found")
  }

  if _, err := os.Stat(ova); os.IsNotExist(err) {
    return nil, false, fmt.Errorf("Failed: No such OVA file '%s'", ova)
  } else {
    // Sweet, we've got an OVA, Now it's time to make that baby something we can work with.
    args := []string{
      "--acceptAllEulas",
      "--lax",
      "--allowAllExtraConfig",
      fmt.Sprintf("--extraConfig:ethernet0.networkName=%s", p.config.VMNetwork),
      fmt.Sprintf("--diskMode=%s", p.config.DiskMode),
      fmt.Sprintf("%s", ova),
      fmt.Sprintf("%s", fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))),
    }

    command := exec.Command("ovftool", args...)

    var ovftoolOut bytes.Buffer
    command.Stdout = &ovftoolOut
    if err := command.Run(); err != nil {
      return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, ovftoolOut.String())
    }

    ui.Message(fmt.Sprintf("%s", ovftoolOut.String()))

    err := filepath.Walk("ova/vmware", visit)
    fmt.Printf("filepath.Walk() returned %v\n", err)

    // for _, path := range artifact.Files() {
    //   if strings.HasSuffix(path, ".ova") {
    //     ova = path
    //     break
    //   } else if strings.HasSuffix(path, ".vmx") {
    //     vmx = path
    //   }
    // }


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

  ui.Message(fmt.Sprintf("Uploading %s and %s to Datastore %s on host %s", vmdk, vmx, p.config.Datastore, p.config.Host))

  clonerequired := false
  if p.config.RemoveEthernet == "false" || p.config.RemoveFloppy == "false" || p.config.RemoveOpticalDrive == "false" {
    clonerequired = true
  }

  splitString := strings.Split(vmdk, "/")
  vmdkDestPath := fmt.Sprintf("folder/%s/%s", p.config.VMFolder, splitString[len(splitString)-1])

  splitString = strings.Split(vmx, "/")
  vmxDestPath := fmt.Sprintf("folder/%s/%s", p.config.VMFolder, splitString[len(splitString)-1])

  err := doUpload(fmt.Sprintf("https://%s:%s@%s/%s?dcPath=%s&dsName=%s",
    url.QueryEscape(p.config.Username),
    url.QueryEscape(p.config.Password),
    p.config.Host,
    vmdkDestPath,
    p.config.Datacenter,
    p.config.Datastore), vmdk)

  if err != nil {
    return nil, false, fmt.Errorf("Failed: %s", err)
  }

  ui.Message(fmt.Sprintf("Uploaded %s", vmdk))

  err = doUpload(fmt.Sprintf("https://%s:%s@%s/%s?dcPath=%s&dsName=%s",
    url.QueryEscape(p.config.Username),
    url.QueryEscape(p.config.Password),
    p.config.Host,
    vmxDestPath,
    p.config.Datacenter,
    p.config.Datastore), vmx)

  if err != nil {
    return nil, false, fmt.Errorf("Failed: %s", err)
  }

  ui.Message(fmt.Sprintf("Uploaded %s", vmx))

  err = doRegistration(ui, p.config, vmx, clonerequired)

  if err != nil {
    return nil, false, fmt.Errorf("Failed: %s", err)
  }
  ui.Message("Uploaded and registered to VMware")

  if p.config.UploadCommand != "" && p.config.UploadArgs != "" {
    bitsupload(ui, p.config, ova)
  }

  return artifact, false, nil
}

func bitsupload(ui packer.Ui, config Config, ova string) (err error) {
  ui.Message(fmt.Sprintf("Please wait, Uploading %s to the bits repo.", ova))
    var out bytes.Buffer
    log.Printf("Starting upload with parameters: %s %s", config.UploadCommand, config.UploadArgs)
    cmd := exec.Command(config.UploadCommand, config.UploadArgs)
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
      return fmt.Errorf("Failed: %s\nStdout: %s", err, out.String())
    }

    ui.Message(fmt.Sprintf("%s", out.String()))

    return nil
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

func doRegistration(ui packer.Ui, config Config, vmx string, clonerequired bool ) (err error) {

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
  datacenter, err := finder.DefaultDatacenter(context.TODO())
  finder.SetDatacenter(datacenter)
  if err != nil {
    return err
  }

  folders, err := datacenter.Folders(context.TODO())
  if err != nil {
    return err
  }

  resourcePool, err := finder.DefaultResourcePool(context.TODO())

  if err != nil {
    return err
  }

  splitString := strings.Split(vmx, "/")
  last := splitString[len(splitString)-1]
  vmName := strings.TrimSuffix(last, ".vmx")

  datastoreString := fmt.Sprintf( "[%s] %s/%s.vmx", config.Datastore, config.VMFolder, vmName )

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

    time.Sleep(150000 * time.Millisecond) // This is really dirty, but I need to make sure the VM gets fully powered on before I turn it off, otherwise vmware tools won't register on the cloning side.

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
