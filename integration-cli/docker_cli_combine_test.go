package main

import (
	"fmt"

	"github.com/go-check/check"
)

func (s *DockerSuite) TestCombine(c *check.C) {
	app1 := "app1:v1.0.0"
	app2 := "app2:v2.0.0"
	app3 := "app3:v3.0.0"
	fullNameApp1 := fmt.Sprintf("%v/dockercli/%v", privateRegistryURL, app1)
	fullNameApp2 := fmt.Sprintf("%v/dockercli/%v", privateRegistryURL, app2)
	fullNameApp3 := fmt.Sprintf("%v/dockercli/%v", privateRegistryURL, app3)

	buildImageFrom(c, "busybox", fullNameApp1, false)
	buildImageFrom(c, fullNameApp1, fullNameApp2, true)
	buildImageFrom(c, fullNameApp2, fullNameApp3, true)

	combinedApp := "app3_v3.0.0-app2_v2.0.0-app1_v1.0.0"
	dockerCmd(c, "combine", fullNameApp3)
	imagesExist(c, true, combinedApp)
	dockerCmd(c, "rmi", combinedApp)
	imagesExist(c, false, combinedApp)

	taggedApp := "taggedapp:1"
	dockerCmd(c, "combine", "-t", taggedApp, fullNameApp3)
	imagesExist(c, true, taggedApp)
	dockerCmd(c, "rmi", taggedApp, fullNameApp1, fullNameApp2, fullNameApp3)
	imagesExist(c, false, taggedApp, fullNameApp1, fullNameApp2, fullNameApp3)
}
