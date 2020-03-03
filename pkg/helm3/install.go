package helm3

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
)

type InstallAction struct {
	Steps []InstallStep `yaml:"install"`
}

type InstallStep struct {
	InstallArguments `yaml:"helm3"`
}

type InstallArguments struct {
	Step `yaml:",inline"`

	Namespace    string                         `yaml:"namespace"`
	Name         string                         `yaml:"name"`
	Chart        string                         `yaml:"chart"`
	Version      string                         `yaml:"version"`
	Replace      bool                           `yaml:"replace"`
	Set          map[string]string              `yaml:"set"`
	Values       []string                       `yaml:"values"`
	Devel        bool                           `yaml:"devel`
	Wait         bool                           `yaml:"wait"`
	Repositories map[string]RepositoryArguments `yaml:"repos"`
}

type RepositoryArguments struct {
	URL      string `yaml:"url"`
	Cafile   string `yaml:"cafile"`
	Certfile string `yaml:"certfile"`
	Keyfile  string `yaml:"keyfile"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func (m *Mixin) Install() error {

	payload, err := m.getPayloadData()
	if err != nil {
		return err
	}

	kubeClient, err := m.getKubernetesClient("/root/.kube/config")
	if err != nil {
		return errors.Wrap(err, "couldn't get kubernetes client")
	}

	var action InstallAction
	err = yaml.Unmarshal(payload, &action)
	if err != nil {
		return err
	}
	if len(action.Steps) != 1 {
		return errors.Errorf("expected a single step, but got %d", len(action.Steps))
	}
	step := action.Steps[0]

	cmd := m.NewCommand("helm3", "install", step.Name, step.Chart)

	if step.Namespace != "" {
		cmd.Args = append(cmd.Args, "--namespace", step.Namespace)
	}

	if step.Version != "" {
		cmd.Args = append(cmd.Args, "--version", step.Version)
	}

	if step.Replace {
		cmd.Args = append(cmd.Args, "--replace")
	}

	if step.Wait {
		cmd.Args = append(cmd.Args, "--wait")
	}

	if step.Devel {
		cmd.Args = append(cmd.Args, "--devel")
	}

	for _, v := range step.Values {
		cmd.Args = append(cmd.Args, "--values", v)
	}

	cmd.Args = HandleSettingChartValues(step, cmd)

	cmdRepos := m.NewCommand("helm3")

	// repos:
	//   brigade:
	//     url: https://azure.github.io/brigade-charts
	for name, repo := range step.Repositories {
		AddRepository(cmdRepos, name, repo.URL, repo.Cafile, repo.Certfile, repo.Keyfile, repo.Username, repo.Password)
	}

	cmd.Stdout = m.Out
	cmd.Stderr = m.Err

	// format the command with all arguments
	prettyCmd := fmt.Sprintf("%s %s", cmd.Path, strings.Join(cmd.Args, " "))
	fmt.Fprintln(m.Out, prettyCmd)

	// Here where really the command get executed
	err = cmd.Start()
	// Exit on error
	if err != nil {
		return fmt.Errorf("could not execute command, %s: %s", prettyCmd, err)
	}
	err = cmd.Wait()
	// Exit on error
	if err != nil {
		return err
	}
	// Handle outputs that where generate throw out the steps
	for _, output := range step.Outputs {
		val, err := getSecret(kubeClient, step.Namespace, output.Secret, output.Key)
		if err != nil {
			return err
		}

		err = m.Context.WriteMixinOutputToFile(output.Name, val)
		if err != nil {
			return errors.Wrapf(err, "unable to write output '%s'", output.Name)
		}
	}

	return nil
}

// Construct and add the request repository
func AddRepository(cmdAddRepo *exec.Cmd, name, url, cafile, certfile, keyfile, username, password string) error {

	if name == "" && url != "" {
		return fmt.Errorf("empty field name")
	}
	cmdAddRepo.Args = append(cmdAddRepo.Args, "repo", "add", name, url)
	if certfile != "" && keyfile != "" {
		cmdAddRepo.Args = append(cmdAddRepo.Args, "--cert-file", certfile, "--key-file", keyfile)
	}
	if cafile != "" {
		cmdAddRepo.Args = append(cmdAddRepo.Args, "--ca-file", cafile)
	}
	if username != "" && password != "" {
		cmdAddRepo.Args = append(cmdAddRepo.Args, "--username", username, "--password", password)
	}
	fmt.Sprintf("Adding repo %v %v", name, url)

	err := cmdAddRepo.Run()
	if err != nil {
		return errors.Wrapf(err, "unable to add the requested repository '%s'", name)
	}
	return err
}

// Prepare set arguments
func HandleSettingChartValues(step InstallStep, cmd *exec.Cmd) []string {
	// sort the set consistently
	setKeys := make([]string, 0, len(step.Set))
	for k := range step.Set {
		setKeys = append(setKeys, k)
	}
	sort.Strings(setKeys)

	for _, k := range setKeys {
		cmd.Args = append(cmd.Args, "--set", fmt.Sprintf("%s=%s", k, step.Set[k]))
	}
	return cmd.Args
}
