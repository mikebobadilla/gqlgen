package modelgen

import (
	"go/types"
	"sort"

	"github.com/99designs/gqlgen/internal/code"

	"github.com/99designs/gqlgen/codegen/config"
	"github.com/99designs/gqlgen/codegen/templates"
	"github.com/99designs/gqlgen/plugin"
	"github.com/vektah/gqlparser/ast"
)

type ModelBuild struct {
	PackageName string
	Interfaces  []*Interface
	Models      []*Object
	Enums       []*Enum
}

type Interface struct {
	Description string
	Name        string
}

type Object struct {
	Description string
	Name        string
	Fields      []*Field
	Implements  []string
}

type Field struct {
	Description string
	Name        string
	Type        types.Type
	Tag         string
}

type Enum struct {
	Description string
	Name        string
	Raw         string
	Values      []*EnumValue
}

type EnumValue struct {
	Description string
	Name        string
	Value       string
}

func New() plugin.Plugin {
	return &Plugin{}
}

type Plugin struct{}

var _ plugin.ConfigMutator = &Plugin{}

func (m *Plugin) Name() string {
	return "modelgen"
}

func (m *Plugin) MutateConfig(cfg *config.Config) error {
	if err := cfg.Check(); err != nil {
		return err
	}

	schema, _, err := cfg.LoadSchema()
	if err != nil {
		return err
	}

	cfg.InjectBuiltins(schema)

	binder, err := cfg.NewBinder(schema)
	if err != nil {
		return err
	}

	b := &ModelBuild{
		PackageName: cfg.Model.Package,
	}

	for _, schemaType := range schema.Types {
		if cfg.Models.UserDefined(schemaType.Name) {
			continue
		}

		switch schemaType.Kind {
		case ast.Interface, ast.Union:
			it := &Interface{
				Description: schemaType.Description,
				Name:        templates.ToGo(schemaType.Name),
			}

			b.Interfaces = append(b.Interfaces, it)
		case ast.Object, ast.InputObject:
			if schemaType == schema.Query || schemaType == schema.Mutation || schemaType == schema.Subscription {
				continue
			}
			it := &Object{
				Description: schemaType.Description,
				Name:        templates.ToGo(schemaType.Name),
			}

			for _, implementor := range schema.GetImplements(schemaType) {
				it.Implements = append(it.Implements, templates.ToGo(implementor.Name))
			}

			for _, field := range schemaType.Fields {
				var typ types.Type

				if cfg.Models.UserDefined(field.Type.Name()) {
					pkg, typeName := code.PkgAndType(cfg.Models[field.Type.Name()].Model[0])
					typ, err = binder.FindType(pkg, typeName)
					if err != nil {
						return err
					}
				} else {
					// no user defined model, must reference another generated model
					typ = types.NewNamed(
						types.NewTypeName(0, cfg.Model.Pkg(), templates.ToGo(field.Type.Name()), nil),
						nil,
						nil,
					)
				}

				name := field.Name
				if nameOveride := cfg.Models[schemaType.Name].Fields[field.Name].FieldName; nameOveride != "" {
					name = nameOveride
				}

				fd := schema.Types[field.Type.Name()]
				it.Fields = append(it.Fields, &Field{
					Name:        templates.ToGo(name),
					Type:        binder.CopyModifiersFromAst(field.Type, fd.Kind != ast.Interface, typ),
					Description: field.Description,
					Tag:         `json:"` + field.Name + `"`,
				})
			}

			b.Models = append(b.Models, it)
		case ast.Enum:
			it := &Enum{
				Name:        templates.ToGo(schemaType.Name),
				Raw:         schemaType.Name,
				Description: schemaType.Description,
			}

			for _, v := range schemaType.EnumValues {
				it.Values = append(it.Values, &EnumValue{
					Name:        templates.ToGo(v.Name),
					Value:       v.Name,
					Description: v.Description,
				})
			}

			b.Enums = append(b.Enums, it)
		}
	}

	sort.Slice(b.Enums, func(i, j int) bool { return b.Enums[i].Name < b.Enums[j].Name })
	sort.Slice(b.Models, func(i, j int) bool { return b.Models[i].Name < b.Models[j].Name })
	sort.Slice(b.Interfaces, func(i, j int) bool { return b.Interfaces[i].Name < b.Interfaces[j].Name })

	for _, it := range b.Enums {
		cfg.Models.Add(it.Raw, cfg.Model.ImportPath()+"."+it.Name)
	}
	for _, it := range b.Models {
		cfg.Models.Add(it.Name, cfg.Model.ImportPath()+"."+it.Name)
	}
	for _, it := range b.Interfaces {
		cfg.Models.Add(it.Name, cfg.Model.ImportPath()+"."+it.Name)
	}

	if len(b.Models) == 0 && len(b.Enums) == 0 {
		return nil
	}

	return templates.Render(templates.Options{
		PackageName:     cfg.Model.Package,
		Filename:        cfg.Model.Filename,
		Data:            b,
		GeneratedHeader: true,
	})
}
