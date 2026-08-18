package model

import (
	"sort"
)

const (
	analyticsNodeW   = 200.0
	analyticsNodeH   = 148.0 // tall enough for metrics; layout uses this for spacing
	analyticsColGap  = 72.0  // horizontal gap between columns
	analyticsRowGap  = 56.0  // vertical gap between cards
	analyticsPadX    = 56.0
	analyticsPadY    = 48.0
)

// layoutAnalyticsCanvasPositions assigns non-overlapping left-to-right columns
// based on graph topology (not builder drag positions).
func layoutAnalyticsCanvasPositions(graph WorkflowGraph) (positions map[string][2]float64, width, height float64) {
	positions = map[string][2]float64{}
	if len(graph.Nodes) == 0 {
		return positions, 1200, 560
	}

	nodeMap := map[string]WorkflowNode{}
	for _, n := range graph.Nodes {
		nodeMap[n.NodeKey] = n
	}

	preds := map[string][]WorkflowEdge{}
	succs := map[string][]WorkflowEdge{}
	for _, e := range graph.Edges {
		preds[e.TargetNodeKey] = append(preds[e.TargetNodeKey], e)
		succs[e.SourceNodeKey] = append(succs[e.SourceNodeKey], e)
	}
	for k := range preds {
		sort.Slice(preds[k], func(i, j int) bool {
			return branchSortKey(preds[k][i]) < branchSortKey(preds[k][j])
		})
	}
	for k := range succs {
		sort.Slice(succs[k], func(i, j int) bool {
			return branchSortKey(succs[k][i]) < branchSortKey(succs[k][j])
		})
	}

	entry := ""
	for _, n := range graph.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entry = n.NodeKey
			break
		}
	}
	if entry == "" {
		entry = graph.Nodes[0].NodeKey
	}

	// Longest-path layer so merges sit after all incoming branches.
	layer := map[string]int{}
	indeg := map[string]int{}
	for _, n := range graph.Nodes {
		indeg[n.NodeKey] = len(preds[n.NodeKey])
		layer[n.NodeKey] = 0
	}
	queue := make([]string, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		if indeg[n.NodeKey] == 0 {
			queue = append(queue, n.NodeKey)
		}
	}
	if len(queue) == 0 {
		queue = append(queue, entry)
	}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, e := range succs[cur] {
			next := e.TargetNodeKey
			if layer[cur]+1 > layer[next] {
				layer[next] = layer[cur] + 1
			}
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	// Cycles / unreachable: pin remaining near entry layer.
	if visited < len(graph.Nodes) {
		for _, n := range graph.Nodes {
			if _, ok := layer[n.NodeKey]; !ok {
				layer[n.NodeKey] = 0
			}
		}
	}
	// Ensure entry is layer 0 and shift if needed.
	if shift := layer[entry]; shift > 0 {
		for k := range layer {
			layer[k] -= shift
			if layer[k] < 0 {
				layer[k] = 0
			}
		}
	}

	maxLayer := 0
	byLayer := map[int][]string{}
	for _, n := range graph.Nodes {
		L := layer[n.NodeKey]
		if L > maxLayer {
			maxLayer = L
		}
		byLayer[L] = append(byLayer[L], n.NodeKey)
	}

	// Preferred vertical lane from incoming branch type + parent order.
	laneHint := map[string]float64{}
	laneHint[entry] = 0
	for L := 0; L <= maxLayer; L++ {
		keys := byLayer[L]
		sort.SliceStable(keys, func(i, j int) bool {
			ai, aj := keys[i], keys[j]
			hi, hj := nodeLaneHint(ai, preds, laneHint), nodeLaneHint(aj, preds, laneHint)
			if hi != hj {
				return hi < hj
			}
			return ai < aj
		})
		byLayer[L] = keys
		for i, k := range keys {
			laneHint[k] = float64(i)
			// Propagate to children not yet ordered.
			for _, e := range succs[k] {
				if _, ok := laneHint[e.TargetNodeKey]; !ok {
					laneHint[e.TargetNodeKey] = float64(i) + branchLaneOffset(e.EdgeType)
				}
			}
		}
	}

	// Barycentric smoothing (2 passes) to reduce crossings.
	for pass := 0; pass < 2; pass++ {
		for L := 1; L <= maxLayer; L++ {
			keys := byLayer[L]
			type scored struct {
				key string
				avg float64
			}
			scoredKeys := make([]scored, 0, len(keys))
			for _, k := range keys {
				sum := 0.0
				n := 0
				for _, e := range preds[k] {
					if idx := indexOfKey(byLayer[layer[e.SourceNodeKey]], e.SourceNodeKey); idx >= 0 {
						sum += float64(idx)
						n++
					}
				}
				avg := laneHint[k]
				if n > 0 {
					avg = sum / float64(n)
				}
				scoredKeys = append(scoredKeys, scored{k, avg})
			}
			sort.SliceStable(scoredKeys, func(i, j int) bool {
				if scoredKeys[i].avg != scoredKeys[j].avg {
					return scoredKeys[i].avg < scoredKeys[j].avg
				}
				return scoredKeys[i].key < scoredKeys[j].key
			})
			keys = keys[:0]
			for _, s := range scoredKeys {
				keys = append(keys, s.key)
			}
			byLayer[L] = keys
		}
	}

	colStride := analyticsNodeW + analyticsColGap
	rowStride := analyticsNodeH + analyticsRowGap

	// Tallest column determines vertical centering baseline.
	maxCount := 1
	for L := 0; L <= maxLayer; L++ {
		if len(byLayer[L]) > maxCount {
			maxCount = len(byLayer[L])
		}
	}
	totalH := float64(maxCount)*analyticsNodeH + float64(maxCount-1)*analyticsRowGap

	for L := 0; L <= maxLayer; L++ {
		keys := byLayer[L]
		colH := float64(len(keys))*analyticsNodeH + float64(max(0, len(keys)-1))*analyticsRowGap
		startY := analyticsPadY + (totalH-colH)/2
		x := analyticsPadX + float64(L)*colStride
		for i, k := range keys {
			y := startY + float64(i)*rowStride
			positions[k] = [2]float64{x, y}
		}
	}

	width = analyticsPadX*2 + float64(maxLayer+1)*analyticsNodeW + float64(maxLayer)*analyticsColGap
	height = analyticsPadY*2 + totalH
	if width < 1200 {
		width = 1200
	}
	if height < 560 {
		height = 560
	}
	return positions, width, height
}

func branchSortKey(e WorkflowEdge) int {
	switch e.EdgeType {
	case "hot", "true":
		return 0
	case "warm":
		return 1
	case "cold", "false":
		return 2
	default:
		if e.Priority > 0 {
			return 10 + e.Priority
		}
		return 5
	}
}

func branchLaneOffset(edgeType string) float64 {
	switch edgeType {
	case "hot", "true":
		return -0.15
	case "warm":
		return 0
	case "cold", "false":
		return 0.15
	default:
		return 0
	}
}

func nodeLaneHint(key string, preds map[string][]WorkflowEdge, laneHint map[string]float64) float64 {
	if v, ok := laneHint[key]; ok {
		return v
	}
	if len(preds[key]) == 0 {
		return 0
	}
	sum := 0.0
	for _, e := range preds[key] {
		sum += laneHint[e.SourceNodeKey] + branchLaneOffset(e.EdgeType)
	}
	return sum / float64(len(preds[key]))
}

func indexOfKey(keys []string, key string) int {
	for i, k := range keys {
		if k == key {
			return i
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
