package core

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"

	"github.com/kyokomi/emoji"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Component struct {
	Name   string
	Source string
	Method string

	Generator     string
	Subcomponents []Component
	Repo          string
	Path          string
	PhysicalPath  string
	LogicalPath   string
	Config        ComponentConfig

	Manifest string
}

func (c *Component) LoadComponent() (mergedComponent Component, err error) {
	componentJSONPath := path.Join(c.PhysicalPath, "component.json")
	if _, err := os.Stat(componentJSONPath); os.IsNotExist(err) {
		return mergedComponent, errors.Errorf("Component expected at path %s not found", componentJSONPath)
	}

	componentJSON, err := ioutil.ReadFile(componentJSONPath)
	if err != nil {
		return mergedComponent, err
	}

	if err := json.Unmarshal(componentJSON, &mergedComponent); err != nil {
		return mergedComponent, err
	}

	mergedComponent.PhysicalPath = c.PhysicalPath
	mergedComponent.LogicalPath = c.LogicalPath
	mergedComponent.Config.Merge(c.Config)

	return mergedComponent, err
}

func (c *Component) MergeConfigFile(path string) (err error) {
	// If config file doesn't exist, just move on.  Config files are never required.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	configString, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	var componentConfig ComponentConfig
	if err := json.Unmarshal(configString, &componentConfig); err != nil {
		return err
	}

	c.Config.Merge(componentConfig)

	return nil
}

func (c *Component) LoadConfig(environment string) (err error) {
	environmentFileName := fmt.Sprintf("%s.json", environment)
	environmentConfigPath := path.Join(c.PhysicalPath, "config", environmentFileName)
	if err := c.MergeConfigFile(environmentConfigPath); err != nil {
		return err
	}

	commonPath := path.Join(c.PhysicalPath, "config", "common.json")
	if err := c.MergeConfigFile(commonPath); err != nil {
		return err
	}

	return nil
}

func (c *Component) RelativePathTo() string {
	if c.Method == "git" {
		return fmt.Sprintf("components/%s", c.Name)
	} else if c.Source != "" {
		return c.Name
	} else {
		return "./"
	}
}

func (c *Component) Install(componentPath string) (err error) {
	for _, subcomponent := range c.Subcomponents {
		if subcomponent.Method == "git" {
			componentsPath := fmt.Sprintf("%s/components", componentPath)
			if err := exec.Command("mkdir", "-p", componentsPath).Run(); err != nil {
				return err
			}

			subcomponentPath := path.Join(componentPath, subcomponent.RelativePathTo())
			if err = exec.Command("rm", "-rf", subcomponentPath).Run(); err != nil {
				return err
			}

			log.Println(emoji.Sprintf(":helicopter: installing component %s with git from %s", subcomponent.Name, subcomponent.Source))
			if err = exec.Command("git", "clone", subcomponent.Source, subcomponentPath).Run(); err != nil {
				return err
			}
		}
	}

	return nil
}

type ComponentIteration func(path string, component *Component) (err error)

// IterateComponentTree is a general function used for iterating a deployment tree for installing, generating, etc.

// It takes a starting path that is expected to have a component.json in it. It is assumed to be an error in this step of
// any path that is pushed onto the queue when component.json does not exist in the path.

// For each component path in the queue, it parses the component at that path into a Component, calls componentFunc on that,
// and then for each subcomponent specified it determines if it is a simple subdirectory of if it (<subcomponent path>) is
// an installed component in components and requires a two level path addition (components/<subcomponent name>).

func IterateComponentTree(startingPath string, environment string, componentIteration ComponentIteration) (completedComponents []Component, err error) {
	queue := make([]Component, 0)

	component := Component{
		PhysicalPath: startingPath,
		LogicalPath:  "./",
		Config: ComponentConfig{
			Config:        make(map[string]interface{}),
			Subcomponents: make(map[string]ComponentConfig),
		},
	}

	queue = append(queue, component)
	completedComponents = make([]Component, 0)

	for len(queue) != 0 {
		component := queue[0]
		queue = queue[1:]

		component, err := component.LoadComponent()
		if err != nil {
			return nil, err
		}

		if err := component.LoadConfig(environment); err != nil {
			return nil, err
		}

		if err = componentIteration(component.PhysicalPath, &component); err != nil {
			return nil, err
		}

		completedComponents = append(completedComponents, component)

		for _, subcomponent := range component.Subcomponents {
			// if subcomponent is inlined, it doesn't need further processing and we are done.
			if subcomponent.Source == "" {
				continue
			}

			componentToQueue := Component{
				Name:         subcomponent.Name,
				PhysicalPath: path.Join(component.PhysicalPath, subcomponent.RelativePathTo()),
				LogicalPath:  path.Join(component.LogicalPath, subcomponent.Name),
				Config:       component.Config.Subcomponents[subcomponent.Name],
			}

			log.Debugf("adding subcomponent '%s' to queue with physical path '%s' and logical path '%s'\n", componentToQueue.Name, componentToQueue.PhysicalPath, componentToQueue.LogicalPath)
			queue = append(queue, componentToQueue)
		}
	}

	return completedComponents, nil
}