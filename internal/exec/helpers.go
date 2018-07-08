package exec

import (
	"context"
	"reflect"

	"github.com/graph-gophers/graphql-go/errors"
	"github.com/graph-gophers/graphql-go/internal/common"
	"github.com/graph-gophers/graphql-go/internal/exec/resolvable"
	"github.com/graph-gophers/graphql-go/internal/exec/selected"
	pubselected "github.com/graph-gophers/graphql-go/selected"
)

func makePanicError(value interface{}) *errors.QueryError {
	return errors.Errorf("graphql: panic occurred: %v", value)
}

func collectFieldsToResolve(sels []selected.Selection, resolver reflect.Value, fields *[]*fieldToExec, fieldByAlias map[string]*fieldToExec) {
	for _, sel := range sels {
		switch sel := sel.(type) {
		case *selected.SchemaField:
			field, ok := fieldByAlias[sel.Alias]
			if !ok { // validation already checked for conflict (TODO)
				field = &fieldToExec{field: sel, resolver: resolver}
				fieldByAlias[sel.Alias] = field
				*fields = append(*fields, field)
			}
			field.sels = append(field.sels, sel.Sels...)

		case *selected.TypenameField:
			sf := &selected.SchemaField{
				Field:       resolvable.MetaFieldTypename,
				Alias:       sel.Alias,
				FixedResult: reflect.ValueOf(typeOf(sel, resolver)),
			}
			*fields = append(*fields, &fieldToExec{field: sf, resolver: resolver})

		case *selected.TypeAssertion:
			out := resolver.Method(sel.MethodIndex).Call(nil)
			if !out[1].Bool() {
				continue
			}
			collectFieldsToResolve(sel.Sels, out[0], fields, fieldByAlias)

		default:
			panic("unreachable")
		}
	}
}

func typeOf(tf *selected.TypenameField, resolver reflect.Value) string {
	if len(tf.TypeAssertions) == 0 {
		return tf.Name
	}
	for name, a := range tf.TypeAssertions {
		out := resolver.Method(a.MethodIndex).Call(nil)
		if out[1].Bool() {
			return name
		}
	}
	return ""
}

func execFieldSelection(ctx context.Context, r *Request, f *fieldToExec, path *pathSegment, applyLimiter bool) {
	if applyLimiter {
		r.Limiter <- struct{}{}
	}

	var result reflect.Value
	var err *errors.QueryError

	traceCtx, finish := r.Tracer.TraceField(ctx, f.field.TraceLabel, f.field.TypeName, f.field.Name, !f.field.Async, f.field.Args)
	defer func() {
		finish(err)
	}()

	err = func() (err *errors.QueryError) {
		defer func() {
			if panicValue := recover(); panicValue != nil {
				r.Logger.LogPanic(ctx, panicValue)
				err = makePanicError(panicValue)
				err.Path = path.toSlice()
			}
		}()

		if f.field.FixedResult.IsValid() {
			result = f.field.FixedResult
			return nil
		}

		if err := traceCtx.Err(); err != nil {
			return errors.Errorf("%s", err) // don't execute any more resolvers if context got cancelled
		}

		if f.field.MethodIndex != -1 {
			var in []reflect.Value
			if f.field.HasContext {
				// lazily evaluate
				resCtx := context.WithValue(traceCtx, pubselected.ContextKey, selectedFields(f.sels))
				in = append(in, reflect.ValueOf(resCtx))
			}
			if f.field.ArgsPacker != nil {
				in = append(in, f.field.PackedArgs)
			}

			callOut := f.resolver.Method(f.field.MethodIndex).Call(in)
			result = callOut[0]
			if f.field.HasError && !callOut[1].IsNil() {
				resolverErr := callOut[1].Interface().(error)
				err := errors.Errorf("%s", resolverErr)
				err.Path = path.toSlice()
				err.ResolverError = resolverErr
				return err
			}
		} else {
			// TODO extract out unwrapping ptr logic to a common place
			res := f.resolver
			if res.Kind() == reflect.Ptr {
				res = res.Elem()
			}
			result = res.Field(f.field.FieldIndex)
		}

		return nil
	}()

	if applyLimiter {
		<-r.Limiter
	}

	if err != nil {
		r.AddError(err)
		f.out.WriteString("null") // TODO handle non-nil
		return
	}
	r.execSelectionSet(traceCtx, f.sels, f.field.Type, path, result, f.out)
}

func unwrapNonNull(t common.Type) (common.Type, bool) {
	if nn, ok := t.(*common.NonNull); ok {
		return nn.OfType, true
	}
	return t, false
}

// lazily add selection fields in context
func selectedFields(sels []selected.Selection) pubselected.SelectedFields {
	return func() []pubselected.SelectedField {
		return selectionToSelectedFields(sels)
	}
}

func selectionToSelectedFields(sels []selected.Selection) []pubselected.SelectedField {
	var selectedFields []pubselected.SelectedField
	selsLen := len(sels)
	if selsLen != 0 {
		selectedFields = make([]pubselected.SelectedField, 0, selsLen)
		for _, sel := range sels {
			selField, ok := sel.(*selected.SchemaField)
			if ok {
				selectedFields = append(selectedFields, pubselected.SelectedField{
					Name:     selField.Field.Name,
					Args:     selField.Args,
					Selected: selectionToSelectedFields(selField.Sels),
				})
			}
		}
	}
	return selectedFields
}

func sendAndReturnClosed(resp *Response) chan *Response {
	c := make(chan *Response, 1)
	c <- resp
	close(c)
	return c
}
