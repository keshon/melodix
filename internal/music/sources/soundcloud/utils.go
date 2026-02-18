package soundcloud

import "strings"

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func MoveToFront(list []string, item string) []string {
	if len(list) == 0 || item == "" {
		return list
	}
	if list[0] == item {
		return list
	}

	ordered := make([]string, 0, len(list))
	ordered = append(ordered, item)

	for _, v := range list {
		if v != item {
			ordered = append(ordered, v)
		}
	}
	return ordered
}
