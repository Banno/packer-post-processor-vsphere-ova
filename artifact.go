package main

import (
  "fmt"
)

const BuilderId = "banno.post-processor.vsphere"

type Artifact struct {
  Name     string
}

func NewArtifact(provider, name string) *Artifact {
  return &Artifact{
    Name:     name,
  }
}

func (*Artifact) BuilderId() string {
  return BuilderId
}

func (a *Artifact) Files() []string {
  return nil
}

func (a *Artifact) Id() string {
  return ""
}

func (a *Artifact) String() string {
  return fmt.Sprintf("%s", a.Name)
}

func (a *Artifact) State(name string) interface{} {
  return nil
}

func (a *Artifact) Destroy() error {
  return nil
}
