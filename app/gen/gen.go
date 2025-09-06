package gen

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/BIwashi/candecode/pkg/dbc"
	"github.com/cockroachdb/errors"
)

// ProtoGenerator generates Proto files from DBC files
type ProtoGenerator struct {
	DBCFile     *dbc.DBCFile
	PackageName string
	logger      slog.Logger
}

// NewProtoGenerator creates a new ProtoGenerator
func NewProtoGenerator(dbcFile *dbc.DBCFile, packageName string, logger slog.Logger) *ProtoGenerator {
	return &ProtoGenerator{
		DBCFile:     dbcFile,
		PackageName: packageName,
		logger:      logger,
	}
}

// GenerateProto generates a Proto file from the DBC file
func (g *ProtoGenerator) GenerateProto(templatePath, outputPath string) error {
	g.logger.Info("Generating proto file", "template_path", templatePath, "output_path", outputPath)

	// Read template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return errors.Wrap(err, "failed to read template file")
	}

	// Create template with helper functions
	funcMap := template.FuncMap{
		"ToProtoMessageName": dbc.ToProtoMessageName,
		"ToProtoFieldName":   dbc.ToProtoFieldName,
		"GetProtoType": func(signal dbc.Signal) string {
			return signal.GetProtoType()
		},
		"add": func(a, b int) int {
			return a + b
		},
	}

	tmpl, err := template.New("proto").Funcs(funcMap).Parse(string(tmplContent))
	if err != nil {
		return errors.Wrap(err, "failed to parse template")
	}

	// Prepare template data
	data := struct {
		PackageName string
		Messages    map[uint32]*dbc.Message
	}{
		PackageName: g.PackageName,
		Messages:    g.DBCFile.Messages,
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return errors.Wrap(err, "failed to execute template")
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create output directory")
	}

	// Write output file
	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		return errors.Wrap(err, "failed to write proto file")
	}

	return nil
}

// GeneratePackageName generates a package name from the DBC filename
func GeneratePackageName(dbcFilename string) string {
	// Get base name without extension
	baseName := filepath.Base(dbcFilename)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// Convert to valid package name (lowercase, replace invalid chars with underscore)
	packageName := strings.ToLower(baseName)
	packageName = strings.ReplaceAll(packageName, "-", "_")
	packageName = strings.ReplaceAll(packageName, " ", "_")

	return packageName
}

// GenerateProtoFilename generates the output proto filename from the DBC filename
func GenerateProtoFilename(dbcFilename string) string {
	// Get base name without extension
	baseName := filepath.Base(dbcFilename)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return baseName + ".proto"
}

// GenerateFromDBCFile is the main entry point for proto generation
func GenerateFromDBCFile(dbcPath, templatePath, outputDir string, logger slog.Logger) error {
	// Parse DBC file (using can-go adapter)
	dbcFile, err := dbc.ParseFile(dbcPath)
	if err != nil {
		return errors.Wrap(err, "failed to parse DBC file")
	}

	// Generate package name and output filename
	packageName := GeneratePackageName(dbcPath)
	protoFilename := GenerateProtoFilename(dbcPath)
	outputPath := filepath.Join(outputDir, protoFilename)

	// Create generator
	generator := NewProtoGenerator(dbcFile, packageName, logger)

	// Generate proto file
	if err := generator.GenerateProto(templatePath, outputPath); err != nil {
		return errors.Wrap(err, "failed to generate proto file")
	}

	logger.Info("Successfully generated proto file", "output_path", outputPath)
	return nil
}
