package heatmap

// Rect represents a rectangle in the terminal grid.
type Rect struct {
	X, Y, W, H int
}

// layoutTreemap computes a squarified treemap layout for n equal-weight sectors.
func layoutTreemap(n, width, height int) []Rect {
	if n == 0 || width <= 0 || height <= 0 {
		return nil
	}

	rects := make([]Rect, n)
	layoutRecursive(rects, 0, n, 0, 0, width, height)
	return rects
}

func layoutRecursive(rects []Rect, start, count, x, y, w, h int) {
	if count <= 0 {
		return
	}
	if count == 1 {
		rects[start] = Rect{X: x, Y: y, W: w, H: h}
		return
	}

	// Split along the longer axis (accounting for terminal aspect ratio ~2:1)
	// Terminal cells are taller than wide, so adjust
	effectiveW := w * 2 // each char is roughly 2x as tall as wide
	horizontal := effectiveW >= h

	// Split count roughly in half
	half := count / 2
	remaining := count - half

	if horizontal {
		// Split width
		leftW := w * half / count
		if leftW < 1 {
			leftW = 1
		}
		rightW := w - leftW
		layoutRecursive(rects, start, half, x, y, leftW, h)
		layoutRecursive(rects, start+half, remaining, x+leftW, y, rightW, h)
	} else {
		// Split height
		topH := h * half / count
		if topH < 1 {
			topH = 1
		}
		botH := h - topH
		layoutRecursive(rects, start, half, x, y, w, topH)
		layoutRecursive(rects, start+half, remaining, x, y+topH, w, botH)
	}
}
