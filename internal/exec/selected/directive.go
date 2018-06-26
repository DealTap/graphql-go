package selected

import (
	"reflect"
	"strings"

	"github.com/graph-gophers/graphql-go/errors"
	"github.com/graph-gophers/graphql-go/internal/common"
	"github.com/graph-gophers/graphql-go/internal/exec/packer"
)

func skipByDirective(r *Request, directives common.DirectiveList) bool {
	if d := directives.Get("skip"); d != nil {
		v, err := extractValue(r, d)
		if err == nil && v.Bool() {
			return true
		}
	}

	if d := directives.Get("include"); d != nil {
		v, err := extractValue(r, d)
		if err == nil && !v.Bool() {
			return true
		}
	}

	return false
}

type StringDirectiveFunc func(string) string

const (
	stringDirectiveUpper = "strings_upper"
	stringDirectiveLower = "strings_lower"
	stringDirectiveTitle = "strings_title"
)

func extractStringDirectives(r *Request, directives common.DirectiveList) []StringDirectiveFunc {
	var stringDirectives []StringDirectiveFunc

	for _, d := range directives {
		if d.Name.Name == stringDirectiveUpper {
			v, err := extractValue(r, d)
			if err == nil && v.Bool() {
				stringDirectives = append(stringDirectives, strings.ToUpper)
			}
		}
		if d.Name.Name == stringDirectiveLower {
			v, err := extractValue(r, d)
			if err == nil && v.Bool() {
				stringDirectives = append(stringDirectives, strings.ToLower)
			}
		}
		if d.Name.Name == stringDirectiveTitle {
			v, err := extractValue(r, d)
			if err == nil && v.Bool() {
				stringDirectives = append(stringDirectives, strings.Title)
			}
		}
	}

	return stringDirectives
}

// TODO: this is just a place holder impl, will decide the direction after auth server research
func authDirective(r *Request, directives common.DirectiveList) bool {
	if d := directives.Get("is_authenticated"); d != nil {
		return false
	}
	return true
}

func extractValue(r *Request, d *common.Directive) (reflect.Value, error) {
	p := packer.ValuePacker{ValueType: reflect.TypeOf(false)}
	v, err := p.Pack(d.Args.MustGet("if").Value(r.Vars))
	if err != nil {
		r.AddError(errors.Errorf("%s", err))
	}
	return v, err
}
