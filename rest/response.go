package rest

import (
	"net/http"

	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
)

var validStatusCodesForDelete = []int{http.StatusOK, http.StatusNoContent, http.StatusAccepted}

// tryPluckingProp does a shallow search for a prop in a map.
// In other words, this only looks for the prop in top-level
// properties and does not go deeper than that.
// If found, returns the value of the prop as well as
// the name of the top-level prop that contained it.
func tryPluckingProp(searchProp string, outputsMap map[string]interface{}) (interface{}, string, bool) {
	var propValue interface{}
	var ok bool

	for k, v := range outputsMap {
		switch prop := v.(type) {
		case map[string]interface{}:
			propValue, ok = prop[searchProp]
			if ok {
				logging.V(3).Infof("found prop %s with value %v in %s", searchProp, propValue, k)
				return propValue, k, ok
			}
		case string:
			// Do nothing.
		default:
			// Do nothing.
		}
	}

	return nil, "", false
}
