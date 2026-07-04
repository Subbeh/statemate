package config

import "gopkg.in/yaml.v3"

func (s *SecretsConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	s.Providers = make(map[string]*SecretsProvider)

	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]

		switch key {
		case "cache":
			s.Cache = value.Value
		default:
			var provider SecretsProvider
			if err := value.Decode(&provider); err != nil {
				return err
			}
			s.Providers[key] = &provider
		}
	}

	return nil
}

func MergeSecretsConfig(base, add *SecretsConfig) *SecretsConfig {
	if add == nil {
		return base
	}
	if base == nil {
		return add
	}

	result := &SecretsConfig{
		Cache:     base.Cache,
		Providers: make(map[string]*SecretsProvider),
	}

	if add.Cache != "" {
		result.Cache = add.Cache
	}

	for name, provider := range base.Providers {
		result.Providers[name] = &SecretsProvider{
			Items: append([]SecretItem{}, provider.Items...),
		}
	}

	for name, provider := range add.Providers {
		if existing, ok := result.Providers[name]; ok {
			existing.Items = append(existing.Items, provider.Items...)
		} else {
			result.Providers[name] = &SecretsProvider{
				Items: append([]SecretItem{}, provider.Items...),
			}
		}
	}

	return result
}
