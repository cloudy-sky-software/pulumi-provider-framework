package rest

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
)

func (p *Provider) TransformBody(ctx context.Context, bodyMap map[string]interface{}, lookupMap map[string]string) {
	if lookupMap == nil || bodyMap == nil {
		return
	}

	for sdkName, v := range bodyMap {
		apiName := sdkName
		if overriddenName, ok := lookupMap[sdkName]; ok {
			apiName = overriddenName
		}

		if mv, ok := v.(map[string]interface{}); ok {
			p.TransformBody(ctx, mv, lookupMap)
			v = mv
		}

		if apiName != sdkName {
			logging.V(7).Infof("sdk name %q of prop is different from api name %q. updating request body", sdkName, apiName)
			delete(bodyMap, sdkName)
		}

		bodyMap[apiName] = v
	}
}

func convertNumericIDToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		logging.V(4).Infof("Value %s to convert is a string already", v)
		return v
	case int:
		logging.V(4).Info("Value to convert is an int")
		return strconv.FormatInt(int64(v), 10)
	case int64:
		logging.V(4).Info("Value to convert is an int64")
		return strconv.FormatInt(v, 10)
	case float64:
		logging.V(4).Info("Value to convert is a float64 which is likely a float with an exponent")
		// It's ok to box this value because we are doing this specifically
		// for resource IDs only which will be integers.
		return strconv.FormatInt(int64(v), 10)
	}

	logging.V(4).Info("Returning default value format")
	return fmt.Sprintf("%v", val)
}
