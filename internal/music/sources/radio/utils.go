package radio

// MoveToFront returns a new slice where `item` is the first element
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
