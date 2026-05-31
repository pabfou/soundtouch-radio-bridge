package config

import (
	_ "embed"
	"log"

	"gopkg.in/yaml.v3"
)

//go:embed factory_profiles.yaml
var factoryProfilesYAML []byte

// factoryProfiles is parsed once at package init. It is treated as immutable.
var factoryProfiles []Profile

func init() {
	var wrapper struct {
		Profiles []Profile `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(factoryProfilesYAML, &wrapper); err != nil {
		log.Fatalf("config: parse factory_profiles.yaml: %v", err)
	}
	factoryProfiles = wrapper.Profiles
}

// FactoryProfiles returns a deep copy of the embedded factory profile set.
func FactoryProfiles() []Profile {
	return cloneProfiles(factoryProfiles)
}

func cloneProfiles(in []Profile) []Profile {
	out := make([]Profile, len(in))
	for i, p := range in {
		out[i] = Profile{
			Name:     p.Name,
			Stations: append([]Station(nil), p.Stations...),
			Presets:  map[int]string{},
		}
		for k, v := range p.Presets {
			out[i].Presets[k] = v
		}
	}
	return out
}
