package rest

import (
	"net/http"

	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
)

var validStatusCodesForDelete = []int{http.StatusOK, http.StatusNoContent, http.StatusAccepted}

func tryPluckingProp(searchProp string, outputsMap map[string]interface{}) (interface{}, bool) {
	var propValue interface{}
	var ok bool

	for k, v := range outputsMap {
		switch prop := v.(type) {
		case map[string]interface{}:
			propValue, ok = prop[searchProp]
			if ok {
				logging.V(3).Infof("found prop %s with value %v in %s", searchProp, propValue, k)
				return propValue, ok
			}
		case string:
			// Do nothing.
		default:
			// Do nothing.
		}
	}

	return propValue, ok
}
