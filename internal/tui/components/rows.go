package components

func RowAt(offset, y, count int) (int, bool) {
	if y < 0 || count <= 0 {
		return 0, false
	}
	row := offset + y
	if row < 0 || row >= count {
		return 0, false
	}
	return row, true
}
