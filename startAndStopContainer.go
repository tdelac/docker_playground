package main

import (
	"fmt"
	"github.com/evergreen-ci/evergreen/command"
	"github.com/fsouza/go-dockerclient"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

const (
	ENDPOINT   = "tcp://10.4.102.195:2376" // Socket for communication with host (2376 required)
	CONFIG     = "config/config.yml"
	HOSTCONFIG = "config/host_config.yml"
	USER       = "root"         // Docker container user
	HOSTIP     = "10.4.102.195" // Machine hosting docker container
)

func readConfig() (*docker.Config, error) {
	yamlIn, err := ioutil.ReadFile(CONFIG)
	if err != nil {
		fmt.Printf("Unable to read config file: %v. %v\n", CONFIG, err)
		return nil, err
	}

	var config docker.Config
	yaml.Unmarshal(yamlIn, &config)
	return &config, nil
}

func readHostConfig() (*docker.HostConfig, error) {
	yamlIn, err := ioutil.ReadFile(HOSTCONFIG)
	if err != nil {
		fmt.Printf("Unable to read config file: %v. %v\n", HOSTCONFIG, err)
		return nil, err
	}

	var hostConfig docker.HostConfig
	yaml.Unmarshal(yamlIn, &hostConfig)
	return &hostConfig, nil
}

func retrieveOpenPortBinding(containerPtr *docker.Container) (docker.PortBinding, error) {
	exposedPorts := containerPtr.Config.ExposedPorts
	for k, _ := range exposedPorts {
		ports := containerPtr.NetworkSettings.Ports
		portBindings := ports[k]
		if len(portBindings) > 0 {
			return portBindings[0], nil
		}
	}
	return docker.PortBinding{}, fmt.Errorf("No available ports")
}

func main() {
	// Initialize client
	client, err := docker.NewTLSClient(ENDPOINT, "cert.pem", "key.pem", "ca.pem")
	if err != nil {
		fmt.Printf("Unable to create client - could be that ENDPOINT is invalid\n")
		return
	}

	// Create container
	config, err := readConfig()
	if err != nil {
		return
	}
	hostConfig, err := readHostConfig()
	if err != nil {
		return
	}
	containerPtr, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name:       "trial",
			Config:     config,
			HostConfig: hostConfig,
		},
	)
	if err != nil {
		fmt.Printf("Failed to create container id: %v. '%v'\n", config.Image, err)
		return
	}

	// Start container
	err = client.StartContainer(containerPtr.ID, nil)
	if err != nil {
		fmt.Printf("Failed to start container %v. %v\n", containerPtr.ID, err)
	}

	// Retrieve relevant info for ssh
	containerPtr, err = client.InspectContainer(containerPtr.ID)
	if err != nil {
		fmt.Printf("Failed to inspect container: %v\n", err)
		return
	}
	portBinding, err := retrieveOpenPortBinding(containerPtr)
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	//	hostIP := portBinding.HostIP
	hostPort := portBinding.HostPort

	// SSH example
	testCmd := &command.RemoteCommand{
		CmdString:      "touch merp.txt",
		Stdout:         ioutil.Discard,
		Stderr:         ioutil.Discard,
		RemoteHostName: HOSTIP,
		User:           USER,
		Options:        []string{"-p", hostPort, "-o", "StrictHostKeyChecking=no"},
		Background:     true,
	}
	err = testCmd.Run()
	if err != nil {
		fmt.Printf("Remote command did not complete successfully: %v\n", err)
		return
	}
}
