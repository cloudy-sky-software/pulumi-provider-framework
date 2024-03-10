package rest

import (
	"context"

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
