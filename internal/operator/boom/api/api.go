package api

import (
	"strings"

	"github.com/caos/orbos/internal/operator/boom/api/common"
	"github.com/caos/orbos/internal/operator/boom/api/latest"
	"github.com/caos/orbos/internal/operator/boom/api/migrate"
	"github.com/caos/orbos/internal/operator/boom/api/v1beta1"
	"github.com/caos/orbos/internal/operator/boom/api/v1beta2"
	"github.com/caos/orbos/internal/operator/boom/metrics"
	"github.com/caos/orbos/pkg/tree"
	"github.com/pkg/errors"
)

const (
	boomPrefix = "caos.ch"
)

func ParseToolset(desiredTree *tree.Tree) (*latest.Toolset, bool, string, string, error) {
	desiredKindCommon := common.New()
	if err := desiredTree.Original.Decode(desiredKindCommon); err != nil {
		metrics.WrongCRDFormat()
		return nil, false, "", "", errors.Wrap(err, "parsing desired state failed")
	}
	if desiredKindCommon.Kind != "Boom" {
		return nil, false, "", "", errors.New("Kind unknown")
	}

	if !strings.HasPrefix(desiredKindCommon.APIVersion, boomPrefix) {
		metrics.UnsupportedAPIGroup()
		return nil, false, "", "", errors.New("Group unknown")
	}

	switch desiredKindCommon.APIVersion {
	case boomPrefix + "/v1beta1":
		old, err := v1beta1.ParseToolset(desiredTree)
		if err != nil {
			return nil, false, "", "", err
		}
		metrics.SuccessfulUnmarshalCRD()
		return migrate.V1beta2Tov1(migrate.V1beta1Tov1beta2(old)), true, desiredKindCommon.Kind, "v1beta1", err
	case boomPrefix + "/v1beta2":
		v1beta2Toolset, err := v1beta2.ParseToolset(desiredTree)
		if err != nil {
			return nil, false, "", "", err
		}
		metrics.SuccessfulUnmarshalCRD()
		return migrate.V1beta2Tov1(v1beta2Toolset), true, desiredKindCommon.Kind, "v1beta2", nil
	case boomPrefix + "/v1":
		desiredKind, err := latest.ParseToolset(desiredTree)
		if err != nil {
			return nil, false, "", "", err
		}
		metrics.SuccessfulUnmarshalCRD()
		return desiredKind, false, desiredKindCommon.Kind, "v1", nil
	default:
		metrics.UnsupportedVersion()
		return nil, false, "", "", errors.New("APIVersion unknown")
	}

}
