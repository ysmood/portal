package utils

// StringSet ...
type StringSet map[string]bool

// Add ...
func (set StringSet) Add(value string) {
	set[value] = true
}

// Has ...
func (set StringSet) Has(value string) bool {
	_, has := set[value]
	return has
}

// Clone ...
func (set StringSet) Clone() StringSet {
	newSet := make(map[string]bool, len(set))
	for k, v := range set {
		newSet[k] = v
	}
	return newSet
}

// ToArray ...
func (set StringSet) ToArray() []string {
	arr := make([]string, len(set))
	i := 0
	for k := range set {
		arr[i] = k
		i++
	}
	return arr
}
