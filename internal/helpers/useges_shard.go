package helpers

import "github.com/GenshIv/makodb/v2"

func CalculateDeltaOffset(old, new []makodb.ShardUsage) (int, int) {
	if len(old) != len(new) {
		return 0, 0
	}
	resMaxDelta := 0
	item := 0
	for i := range new {
		delta := int(new[i].FreeOffset - old[i].FreeOffset)
		if resMaxDelta < delta {
			resMaxDelta = delta
			item = i
		}
	}

	return resMaxDelta, item
}
