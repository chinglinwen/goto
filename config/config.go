package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"k8s.io/klog"
)

type Config struct {
	Creds []struct {
		Name    string `yaml:"name"`
		User    string `yaml:"user"`
		Pass    string `yaml:"pass"`
		Keypath string `yaml:"keypath"`
	} `yaml:"creds"`
	Hosts []struct {
		Name     string `yaml:"name"`
		Host     string `yaml:"host"`
		Cred     string `yaml:"cred"`
		Port     string `yaml:"port"`
		Label    string `yaml:"label"`
		Jump     string `yaml:"jump"`
		InitCmds string `yaml:"initcmds"`
	} `yaml:"hosts"`
}

func ParseConfig() (*Config, error) {
	var lastErr error
	for _, path := range configFiles() {
		v := viper.New()
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			lastErr = err
			continue
		}
		klog.V(2).Info("read from config: ", v.ConfigFileUsed())
		c := &Config{}
		if err := v.Unmarshal(c); err != nil {
			return nil, err
		}
		return c, nil
	}
	return nil, lastErr
}

func configFiles() []string {
	home, _ := os.UserHomeDir()
	files := []string{}
	if home != "" {
		files = append(files,
			filepath.Join(home, ".ssh", "goto.yaml"),
			filepath.Join(home, ".goterm", "config.yaml"),
		)
	}
	return append(files,
		filepath.Join("/etc/goto", "config.yaml"),
		filepath.Join("/etc/goterm", "config.yaml"),
		"config.yaml",
	)
}

func (c *Config) GetHost(expr string) (host, port, cred, cmds, jump string) {
	klog.V(2).Infof("checking host: %v", expr)
	for _, v := range c.Hosts {
		if v.Name == expr || strings.HasSuffix(v.Host, expr) {
			klog.V(2).Infof("got cred: %+v", v)
			return v.Host, v.Port, v.Cred, v.InitCmds, v.Jump
		}
	}
	return expr, "22", "", "", ""
}

// if name empty, get from default cred
func (c *Config) GetCred(name string) (user, pass, keypath string) {
	klog.V(2).Infof("checking cred: %v", name)
	for _, v := range c.Creds {
		if v.Name == name {
			klog.V(2).Infof("found cred for: %v", name)
			return v.User, v.Pass, v.Keypath
		}
	}
	klog.V(2).Infof("cred not found for: %v", name)
	return
}
