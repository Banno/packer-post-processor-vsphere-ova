package main

import (
  "bytes"
  "fmt"
  "github.com/mitchellh/packer/common"
  "github.com/mitchellh/packer/packer"
  "github.com/vmware/govmomi"
  "github.com/vmware/govmomi/find"
  "golang.org/x/net/context"
  "io/ioutil"
  "net/url"
  "os"
  "os/exec"
  "strings"
  "log"
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
  Cluster            string `mapstructure:"cluster"`
  ResourcePool       string `mapstructure:"resource_pool"`
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
  Insecure           string `mapstructure:"insecure"`
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
  if p.config.DiskMode == "" {
    p.config.DiskMode = "thick"
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

  if p.config.Insecure == "" {
    p.config.Insecure = "false"
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
      errs, fmt.Errorf("Invalid disk_mode. Only thick(Default), thin, monolithicSparse, monolithicFlat, twoGbMaxExtentSparse, twoGbMaxExtentFlat, seSparse, eagerZeroedThick, sparse, and flat are allowed."))
  }

  // First define all our templatable parameters that are _required_
  templates := map[string]*string{
    "datacenter": &p.config.Datacenter,
    "cluster":    &p.config.Cluster,
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
  for _, path := range artifact.Files() {
    if strings.HasSuffix(path, ".ova") {
      ova = path
      break
    } else if strings.HasSuffix(path, ".vmx") {
      vmx = path
      break
    }
  }

  if ova == "" && vmx == "" {
    return nil, false, fmt.Errorf("ERROR: Neither OVA nor VMX were found!")
  }

  if artifact.BuilderId() == "mitchellh.virtualbox" && ova != "" {
    // Sweet, we've got an OVA, Now it's time to make that baby something we can work with.
    args := []string{
      "--acceptAllEulas",
      "--lax",
      "--allowAllExtraConfig",
      fmt.Sprintf("--extraConfig:ethernet0.networkName=%s", p.config.VMNetwork),
      fmt.Sprintf("%s", ova),
      fmt.Sprintf("%s", fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))),
    }

    command := exec.Command("ovftool", args...)

    var ovftoolOut bytes.Buffer
    command.Stdout = &ovftoolOut
    if err := command.Run(); err != nil {
      return nil, false, fmt.Errorf("Failed: %s\nStdout: %s", err, ovftoolOut.String())

      ui.Message(fmt.Sprintf("%s", ovftoolOut.String()))

      // vmdk = fmt.Sprintf("%s-disk1.vmdk", strings.TrimSuffix(ova, ".ova"))
      vmx = fmt.Sprintf("%s.vmx", strings.TrimSuffix(ova, ".ova"))
    }
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

  if err := doVmxImport(ui, p.config, vmx) ; err != nil {
    return nil, false, fmt.Errorf("Failed: %s", err)
  }

  ui.Message(fmt.Sprintf("Uploaded %s", vmx))

  if err := setAsTemplate(ui, p.config, vmx) ; err != nil {
    return nil, false, fmt.Errorf("Failed: %s", err)
  }

  ui.Message("Uploaded and registered to VMware as a template")

  if artifact.BuilderId() == "mitchellh.vmware" && (p.config.UploadCommand != "" && p.config.UploadArgs != ""){
    // Convert vmware builder artifact to ova so we can upload to bits if required.
    if _, err := os.Stat("ova/vmware"); os.IsNotExist(err) {
      os.Mkdir("ova/vmware",0755)
    }

    splitString := strings.Split(vmx, "/")
    ova = fmt.Sprintf("ova/vmware/%s.ova", strings.TrimSuffix(splitString[len(splitString)-1], ".vmx"))

    args := []string{
      "--acceptAllEulas",
      "-tt=OVA",
      "--diskMode=thin",
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

    ui.Message(fmt.Sprintf("Conversion of VMX to OVA: %s", out.String()))
  }

  if p.config.UploadCommand != "" && p.config.UploadArgs != "" {
    if err := doBitsUpload(ui, p.config, ova) ; err != nil {
      return nil, false, fmt.Errorf("Failed: %s", err)
    }
  }

  return artifact, false, nil
}

func doBitsUpload(ui packer.Ui, config Config, ova string) (err error) {
  args := strings.Split(config.UploadArgs, " ")

  ui.Message(fmt.Sprintf("Please wait, Uploading %s to the bits repo.", ova))
  var out bytes.Buffer
  ui.Message(fmt.Sprintf("Starting '%s' with parameters: %s", config.UploadCommand, config.UploadArgs))
  command := exec.Command(config.UploadCommand, args...)
  command.Stdout = &out
  if err := command.Run(); err != nil {
    return fmt.Errorf("Failed: %s\nStdout: %s", err, out.String())
  }

  ui.Message(fmt.Sprintf("%s", out.String()))

  return nil
}

func doVmxImport(ui packer.Ui, config Config, vmx string) (err error) {
  splitString := strings.Split(vmx, "/")
  last := splitString[len(splitString)-1]
  VMName := strings.TrimSuffix(last, ".vmx")

  ovftool_uri := fmt.Sprintf("vi://%s:%s@%s/%s/host/%s",
    url.QueryEscape(config.Username),
    url.QueryEscape(config.Password),
    config.Host,
    config.Datacenter,
    config.Cluster)

  if config.ResourcePool != "" {
    ovftool_uri += "/Resources/" + config.ResourcePool
  }

  args := []string{
    fmt.Sprintf("--noSSLVerify=%s", config.Insecure),
    "--acceptAllEulas",
    fmt.Sprintf("--name=%s", VMName),
    fmt.Sprintf("--datastore=%s", config.Datastore),
    fmt.Sprintf("--diskMode=%s", config.DiskMode),
    fmt.Sprintf("--network=%s", config.VMNetwork),
    fmt.Sprintf("%s", vmx),
    fmt.Sprintf("%s", ovftool_uri),
  }

  ui.Message(fmt.Sprintf("Uploading %s to vSphere", vmx))
  var out bytes.Buffer
  log.Printf("Starting ovftool with parameters: %s", strings.Join(args, " "))
  cmd := exec.Command("ovftool", args...)
  cmd.Stdout = &out
  if err := cmd.Run(); err != nil {
    return fmt.Errorf("Failed: %s\nStdout: %s", err, out.String())
  }

  ui.Message(fmt.Sprintf("%s", out.String()))

  return nil
}

func setAsTemplate(ui packer.Ui, config Config, vmx string ) (err error) {
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

  splitString := strings.Split(vmx, "/")
  last := splitString[len(splitString)-1]
  vmName := strings.TrimSuffix(last, ".vmx")

  vm, err := finder.VirtualMachine(context.TODO(), vmName)

  ui.Message(fmt.Sprintf("Marking as template %s", vmName))
  err = vm.MarkAsTemplate(context.TODO())

  if err != nil {
    return err
  }
  ui.Message(fmt.Sprintf("%s is now a template", vmName))

  return nil
}
