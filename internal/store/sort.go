package store

import "strings"

// ifaceLess orders interface names in natural EOS order so that, e.g.,
// Ethernet2 sorts before Ethernet10 and Ethernet3/1 before Ethernet3/10.
// Non-Ethernet names (Management1, Port-Channel, …) sort after Ethernet.
func ifaceLess(a, b string) bool {
	pa, na, oka := splitIface(a)
	pb, nb, okb := splitIface(b)
	if oka && okb {
		if pa != pb {
			return pa < pb // different prefixes ("Ethernet" vs "Management")
		}
		// compare numeric segments left to right
		for i := 0; i < len(na) && i < len(nb); i++ {
			if na[i] != nb[i] {
				return na[i] < nb[i]
			}
		}
		return len(na) < len(nb)
	}
	if oka != okb {
		return oka // parseable names first
	}
	return a < b
}

// splitIface separates "Ethernet3/1/2" into prefix "Ethernet" and the numeric
// path [3,1,2]. ok is false when no numeric component is present.
func splitIface(s string) (prefix string, nums []int, ok bool) {
	i := strings.IndexFunc(s, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 {
		return s, nil, false
	}
	prefix = s[:i]
	for _, part := range strings.FieldsFunc(s[i:], func(r rune) bool {
		return r == '/' || r == '.' || r == ':'
	}) {
		n := 0
		for _, c := range part {
			if c < '0' || c > '9' {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
		}
		if n >= 0 {
			nums = append(nums, n)
		}
	}
	return prefix, nums, len(nums) > 0
}
