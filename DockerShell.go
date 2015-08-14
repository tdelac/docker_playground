package main

import (
	"./tokenizer"
	"bufio"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"os"
)

const (
	CONFIG      = "./config/shell-config.yml"
	SSHDPort    = "22/tcp"
	KILLTIMEOUT = 5
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

		// Make a client
		client, err := generateClient()
		if err != nil {
			return fmt.Errorf("Cannot connect to Docker server: %v\n", err)
		}

		// Create the container
		containerName := "docker-" + bson.NewObjectId().Hex()
		container, err := client.CreateContainer(
			docker.CreateContainerOptions{
				Name: containerName,
				Config: &docker.Config{
					Cmd: []string{"/usr/sbin/sshd", "-D"},
					ExposedPorts: map[docker.Port]struct{}{
						SSHDPort: struct{}{},
					},
					Image: image,
				},
				// Allow Docker to randomly allocate a port
				HostConfig: &docker.HostConfig{
					PublishAllPorts: true,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("CreateContainer API call failed: %v")
		}
		fmt.Printf("\n")
		fmt.Printf("Created container: %v\n", container.ID)

		// Kick off container
		err = client.StartContainer(container.ID, nil)
		if err != nil {
			// Clean up
			err2 := client.RemoveContainer(
				docker.RemoveContainerOptions{
					ID:    container.ID,
					Force: true,
				},
			)
			if err2 != nil {
				err = fmt.Errorf("%v. And was unable to clean up container %v: %v", err, container.ID, err2)
			}
			return fmt.Errorf("StartContainer API call failed: %v", err)
		}
		fmt.Printf("Started container: %v\n", container.ID)
		fmt.Printf("\n")

		return nil
	}

	return fmt.Errorf("Invalid command\n")
}

func listContainers(tokens *tokenizer.TokenIterator) error {
	opts := docker.ListContainersOptions{}

	// Check for "all" parameter to the list containers command
	if tokens.HasNext() {
		token, err := tokens.Next()
		if err != nil {
			return fmt.Errorf("Could not get next token: %v", err)
		}

		switch token {
		case "all":
			opts.All = true
		default:
			return fmt.Errorf("Invalid argument to `containers` - `%s`", token)
		}
	} else {
		opts.All = false
	}

	// Create connection
	client, err := generateClient()
	if err != nil {
		return fmt.Errorf("Cannot connect to Docker server: %v", err)
	}

	// Retrieve all the containers
	containers, err := client.ListContainers(opts)
	if err != nil {
		return fmt.Errorf("ListContainers API call failed: %v", err)
	}

	// TODO make better ascii table
	fmt.Printf("\n")
	fmt.Printf("ID\t\t\t\t\t\t\t\t\t\tIMAGE\t\t\t\t\tSTATUS\t\t\tPORT\n")
	for _, container := range containers {
		fmt.Printf("%v\t\t%v\t\t%v\t\t%v\n", container.ID, container.Image, container.Status, container.Ports)
	}
	fmt.Printf("\n")

	return nil
}

func killContainer(tokens *tokenizer.TokenIterator) error {
	if tokens.HasNext() {
		token, err := tokens.Next()
		if err != nil {
			return fmt.Errorf("Could not get next token: %v", err)
		}

		// Create connection
		client, err := generateClient()
		if err != nil {
			return fmt.Errorf("Cannot connect to Docker server: %v", err)
		}

		// Retrieve containers currently running
		runningContainers, err := client.ListContainers(
			docker.ListContainersOptions{},
		)
		if err != nil {
			return fmt.Errorf("ListContainers API call failed: %v", err)
		}

		// Retrieve containers currently in existence
		allContainers, err := client.ListContainers(
			docker.ListContainersOptions{
				All: true,
			},
		)
		if err != nil {
			return fmt.Errorf("ListContainers API call failed: %v", err)
		}

		var idsToStop []string
		var idsToKill []string
		switch token {
		case "all":
			idsToStop = make([]string, 0) // hmm, not sure why len(runningContainers) doesnt work
			for _, container := range runningContainers {
				idsToStop = append(idsToStop, container.ID)
			}
			idsToKill = make([]string, 0)
			for _, container := range allContainers {
				idsToKill = append(idsToKill, container.ID)
			}
		default:
			exists := false
			for _, container := range runningContainers {
				if token == container.ID {
					idsToStop = []string{token}
					idsToKill = []string{token}
					exists = true
				}
			}
			if !exists {
				exists = false
				for _, container := range allContainers {
					if token == container.ID {
						idsToKill = []string{token}
						exists = true
						break
					}
				}
				if !exists {
					fmt.Printf("Container ID does not exist\n")
				}
			}
		}

		// Stop containers
		for _, id := range idsToStop {
			if err = client.StopContainer(id, KILLTIMEOUT); err != nil {
				return fmt.Errorf("StopContainer API call failed: %v", err)
			}
		}

		// Remove containers
		for _, id := range idsToKill {
			err = client.RemoveContainer(
				docker.RemoveContainerOptions{
					ID:    id,
					Force: true,
				},
			)
			if err != nil {
				return fmt.Errorf("RemoveContainer API call failed: %v", err)
			}
		}

		return nil
	}

	return fmt.Errorf("Need to specify what to kill")
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
			err = listContainers(tokens)
		case "kill":
			err = killContainer(tokens)
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
