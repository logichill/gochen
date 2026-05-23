package config

import (
	"sort"

	"gochen/errors"
)

// load 按优先级顺序加载并合并所有配置源。
func (p *Provider) load() error {
	// 按优先级排序（低优先级先加载，高优先级后覆盖）
	sortedSources := make([]IConfigSource, len(p.sources))
	copy(sortedSources, p.sources)

	sort.SliceStable(sortedSources, func(i, j int) bool {
		return sortedSources[i].Priority() < sortedSources[j].Priority()
	})

	// 依次加载配置源
	for _, source := range sortedSources {
		settings, err := source.Load()
		if err != nil {
			return errors.Wrap(err, errors.Dependency, "failed to load config source").
				WithContext("source", source.Name())
		}

		// 合并配置
		for k, v := range settings {
			p.settings[k] = v
		}
	}

	return nil
}
