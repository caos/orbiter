package labels

import (
	"errors"

	"gopkg.in/yaml.v3"
)

var _ Labels = (*Component)(nil)

type Component struct {
	model InternalComponent
	base  *API
}

func ForComponent(l *API, component string) (*Component, error) {
	if component == "" {
		return nil, errors.New("component must not be nil")
	}
	return &Component{
		base: l,
		model: InternalComponent{
			InternalComponentProp: InternalComponentProp{Component: component},
			InternalAPI:           l.model,
		},
	}, nil
}

func (l *Component) UnmarshalYAML(node *yaml.Node) error {
	if err := node.Decode(&l.model); err != nil {
		return err
	}
	l.base = &API{}
	return node.Decode(l.base)
}

func MustForComponent(l *API, component string) *Component {

	c, err := ForComponent(l, component)
	if err != nil {
		panic(err)
	}
	return c
}

func MustReplaceComponent(l *Component, component string) *Component {
	return MustForComponent(GetAPIFromComponent(l), component)
}

func GetAPIFromComponent(l *Component) *API {
	return l.base
}

/*
func (l *Component) Major() int8 {
	return l.base.Major()
}
*/
func (l *Component) Equal(r comparable) bool {
	if right, ok := r.(*Component); ok {
		return l.model == right.model
	}
	return false
}

func (l *Component) MarshalYAML() (interface{}, error) {
	return nil, errors.New("type *labels.Component is not serializable")
}

type InternalComponentProp struct {
	Component string `yaml:"app.kubernetes.io/component"`
}

type InternalComponent struct {
	InternalComponentProp `yaml:",inline"`
	InternalAPI           `yaml:",inline"`
}
