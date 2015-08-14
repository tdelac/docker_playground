package main

import (
	"./tokenizer"
	"bufio"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"os"
)

const (
	CONFIG = "./config/shell-config.yml"
)

var (
	config *settings
)

type settings struct {
	Endpoint string `yaml:"endpoint"`
	CertPath string `yaml:"cert_path"`
	KeyPath  string `yaml:"key_path"`
	CaPath   string `yaml:"ca_path"`
}

// Create a connection to the remote Docker server
func generateClient() (*docker.Client, error) {
	client, err := docker.NewTLSClient(config.Endpoint, config.CertPath, config.KeyPath, config.CaPath)
	if err != nil {
		return nil, fmt.Errorf("Unable to create client: %v", err)
	}
	return client, nil
}

// Makes the API call to list available images at the endpoint
func listImages() error {
	// Create the connection
	client, err := generateClient()
	if err != nil {
		return fmt.Errorf("Cannot connect to Docker server: %v\n", err)
	}

	// Retrieve and print image info
	images, err := client.ListImages(
		docker.ListImagesOptions{
			All: true,
		},
	)
	if err != nil {
		return fmt.Errorf("ListImages API call failed: %v", err)
	}

	// Print the results
	// TODO should really make a better ascii table thing
	fmt.Printf("\n")
	fmt.Printf("REPOSITORY:TAG\t\t\tIMAGE ID\n")
	for _, image := range images {
		var repoTag string
		repoTags := image.RepoTags
		if len(repoTags) > 0 {
			repoTag = repoTags[0]
		}

		// Just not very interesting to see
		if repoTag == "<none>:<none>" {
			continue
		}
		fmt.Printf("%v\t\t%v\n", repoTag, image.ID)
	}
	fmt.Printf("\n")

	return nil
}

func spawnContainer(tokens *tokenizer.TokenIterator) error {
	if tokens.HasNext() {
		image, err := tokens.Next()
		if err != nil {
			return fmt.Errorf("Parse error: %v", err)
		}

		_ := &docker.HostConfig{}
		// Populate host config
		// Create container
		// Start container
	}

	return fmt.Errorf("Invalid command\n")
}

func parse(input string) error {
	tokens := tokenizer.New(input)

	if tokens.HasNext() {
		token, err := tokens.Next()
		if err != nil {
			return fmt.Errorf("Parse error: %v", err)
		}

		// Switch over the possible first term in a command
		switch token {
		case "images":
			err = listImages()
		case "run":
			err = spawnContainer(tokens)
		case "containers":
			fmt.Printf("here are all containers\n")
		case "exit":
			return io.EOF
		default:
			fmt.Printf("Token: '%s' retrieved\n", token)
		}

		// Catch all for errors in any of the cases
		if err != nil {
			return fmt.Errorf("Unable to run command `%s`: %v", token, err)
		}
	}

	return nil
}

func quit() {
	fmt.Printf("\nGoodbye!\n")
}

func getSettings() (*settings, error) {
	yamlFile, err := ioutil.ReadFile(CONFIG)
	if err != nil {
		return nil, fmt.Errorf("Error opening config file `%s`: %v", CONFIG, err)
	}

	ret := settings{}
	err = yaml.Unmarshal(yamlFile, &ret)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshalling config file `%s`: %v", CONFIG, err)
	}

	return &ret, nil
}

func main() {
	var err error

	config, err = getSettings()
	if err != nil {
		fmt.Printf("Unable to retrieve settings")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("[DOCKER-SHELL] -> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				quit()
				return
			}
			fmt.Printf("Improper read '%s': %v\n", input, err)
			os.Exit(1)
		}

		// Parse the input line
		err = parse(input)
		if err != nil {
			if err == io.EOF {
				quit()
				return
			}
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	}
}
