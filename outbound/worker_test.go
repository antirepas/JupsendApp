package outbound

import "testing"

func TestFilterJobIDsOnePerUser(t *testing.T) {
	ids := []int64{1, 2, 3, 4, 5}
	lookup := func(id int64) (int64, error) {
		switch id {
		case 1, 2:
			return 10, nil
		case 3:
			return 20, nil
		case 4:
			return 10, nil
		case 5:
			return 30, nil
		}
		return 0, nil
	}
	out := filterJobIDsOnePerUser(ids, lookup)
	if len(out) != 3 {
		t.Fatalf("expected 3 ids, got %d: %v", len(out), out)
	}
	if out[0] != 1 || out[1] != 3 || out[2] != 5 {
		t.Fatalf("unexpected filter result: %v", out)
	}
}
