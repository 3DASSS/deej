package deej

import (
	"fmt"
	"slices"
	"sync"
)

type sliderMap struct {
	m    map[int][]string
	lock sync.Locker
}

func newSliderMap() *sliderMap {
	return &sliderMap{
		m:    make(map[int][]string),
		lock: &sync.Mutex{},
	}
}

func sliderMapFromSettings(mapping SliderMappings) *sliderMap {
	resultMap := newSliderMap()

	// copy targets from the config, ignoring empty values
	for _, entry := range mapping {
		targets := slices.DeleteFunc(slices.Clone(entry.Targets), func(s string) bool { return s == "" })
		resultMap.set(entry.Slider, targets)
	}

	return resultMap
}

func (m *sliderMap) iterate(f func(int, []string)) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for key, value := range m.m {
		f(key, value)
	}
}

func (m *sliderMap) get(key int) ([]string, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	value, ok := m.m[key]
	return value, ok
}

func (m *sliderMap) set(key int, value []string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.m[key] = value
}

func (m *sliderMap) String() string {
	m.lock.Lock()
	defer m.lock.Unlock()

	sliderCount := 0
	targetCount := 0

	for _, value := range m.m {
		sliderCount++
		targetCount += len(value)
	}

	return fmt.Sprintf("<%d sliders mapped to %d targets>", sliderCount, targetCount)
}
