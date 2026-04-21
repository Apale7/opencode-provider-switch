package opencode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	sdk "github.com/sst/opencode-sdk-go"
	"github.com/sst/opencode-sdk-go/option"
)

const DefaultRuntimeBaseURL = "http://localhost:54321/"

type FileProviderSnapshot struct {
	Key                string
	Name               string
	NPM                string
	Protocol           string
	BaseURL            string
	ModelAliases       []string
	MissingFields      []string
	UnknownFieldKeys   []string
	RawJSONFragment    string
	ContractConfigured bool
}

type FileConfigSnapshot struct {
	TargetPath           string
	Exists               bool
	Schema               string
	DefaultModel         string
	SmallModel           string
	ProviderKeys         []string
	ExpectedProtocols    []string
	SyncedProviders      []FileProviderSnapshot
	UnknownTopLevelKeys  []string
	ParseError           string
	DefaultModelRoutable bool
	SmallModelRoutable   bool
}

type RuntimeModelSnapshot struct {
	ID               string
	Name             string
	ProviderID       string
	ProviderNPM      string
	RawJSON          string
	ExtraFieldKeys   []string
	OptionKeys       []string
	Experimental     bool
	Reasoning        bool
	ToolCall         bool
	Temperature      bool
	Attachment       bool
	ContextLimit     int64
	OutputLimit      int64
	ReleaseDate      string
	Status           string
	InputModalities  []string
	OutputModalities []string
}

type RuntimeProviderSnapshot struct {
	ID             string
	Name           string
	API            string
	NPM            string
	Env            []string
	ModelIDs       []string
	Models         []RuntimeModelSnapshot
	ExtraFieldKeys []string
	RawJSON        string
}

type RuntimeConfigSnapshot struct {
	BaseURL               string
	Directory             string
	Reachable             bool
	ConfigLoaded          bool
	ProvidersLoaded       bool
	DefaultModel          string
	SmallModel            string
	ProviderKeys          []string
	DefaultProviderModels map[string]string
	Providers             []RuntimeProviderSnapshot
	ErrorCode             string
	ErrorMessage          string
	HTTPStatus            int
	RawConfigJSON         string
	RawProvidersJSON      string
	ConfigExtraFieldKeys  []string
	ProviderExtraFieldMap map[string][]string
}

type OpenCodeReadResult struct {
	File    FileConfigSnapshot
	Runtime RuntimeConfigSnapshot
}

type RuntimeReadOptions struct {
	BaseURL        string
	Directory      string
	HTTPClient     *http.Client
	Middleware     []option.Middleware
	RequestTimeout time.Duration
	MaxRetries     int
}

func SnapshotFileConfig(targetPath string, existed bool, raw Raw, parseErr error, expectedProtocols []string) FileConfigSnapshot {
	snapshot := FileConfigSnapshot{
		TargetPath:        targetPath,
		Exists:            existed,
		ExpectedProtocols: append([]string(nil), expectedProtocols...),
	}
	if parseErr != nil {
		snapshot.ParseError = parseErr.Error()
		return snapshot
	}
	if raw == nil {
		return snapshot
	}
	snapshot.Schema, _ = raw["$schema"].(string)
	snapshot.DefaultModel, _ = raw["model"].(string)
	snapshot.SmallModel, _ = raw["small_model"].(string)
	providerRaw, _ := raw["provider"].(map[string]any)
	if providerRaw != nil {
		for key := range providerRaw {
			snapshot.ProviderKeys = append(snapshot.ProviderKeys, key)
		}
		sort.Strings(snapshot.ProviderKeys)
	}
	topLevelKnown := map[string]bool{
		"$schema":     true,
		"model":       true,
		"small_model": true,
		"provider":    true,
	}
	for key := range raw {
		if !topLevelKnown[key] {
			snapshot.UnknownTopLevelKeys = append(snapshot.UnknownTopLevelKeys, key)
		}
	}
	sort.Strings(snapshot.UnknownTopLevelKeys)
	for _, protocol := range expectedProtocols {
		providerKey := syncedProviderKey(protocol)
		providerEntry, _ := providerRaw[providerKey].(map[string]any)
		snapshot.SyncedProviders = append(snapshot.SyncedProviders, snapshotFileProvider(protocol, providerKey, providerEntry))
	}
	sort.Slice(snapshot.SyncedProviders, func(i, j int) bool { return snapshot.SyncedProviders[i].Key < snapshot.SyncedProviders[j].Key })
	return snapshot
}

func ReadRuntimeConfig(ctx context.Context, in RuntimeReadOptions) RuntimeConfigSnapshot {
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = DefaultRuntimeBaseURL
	}
	snapshot := RuntimeConfigSnapshot{
		BaseURL:               baseURL,
		Directory:             strings.TrimSpace(in.Directory),
		DefaultProviderModels: map[string]string{},
		ProviderExtraFieldMap: map[string][]string{},
	}
	requestOptions := []option.RequestOption{option.WithBaseURL(baseURL)}
	if in.HTTPClient != nil {
		requestOptions = append(requestOptions, option.WithHTTPClient(in.HTTPClient))
	}
	if in.RequestTimeout > 0 {
		requestOptions = append(requestOptions, option.WithRequestTimeout(in.RequestTimeout))
	}
	if in.MaxRetries >= 0 {
		requestOptions = append(requestOptions, option.WithMaxRetries(in.MaxRetries))
	}
	if len(in.Middleware) > 0 {
		requestOptions = append(requestOptions, option.WithMiddleware(in.Middleware...))
	}
	client := sdk.NewClient(requestOptions...)
	configResp, err := client.Config.Get(ctx, sdk.ConfigGetParams{}, runtimeRequestOptions(snapshot.Directory)...)
	if err != nil {
		applyRuntimeError(&snapshot, err)
		return snapshot
	}
	providersResp, err := client.App.Providers(ctx, sdk.AppProvidersParams{}, runtimeRequestOptions(snapshot.Directory)...)
	if err != nil {
		applyRuntimeError(&snapshot, err)
		snapshot.Reachable = true
		snapshot.ConfigLoaded = true
		snapshot.DefaultModel = strings.TrimSpace(configResp.Model)
		snapshot.SmallModel = strings.TrimSpace(configResp.SmallModel)
		snapshot.ProviderKeys = sortedMapKeys(configResp.Provider)
		snapshot.RawConfigJSON = configResp.JSON.RawJSON()
		snapshot.ConfigExtraFieldKeys = extraFieldKeys(configResp.JSON.ExtraFields)
		return snapshot
	}
	snapshot.Reachable = true
	snapshot.ConfigLoaded = true
	snapshot.ProvidersLoaded = true
	snapshot.DefaultModel = strings.TrimSpace(configResp.Model)
	snapshot.SmallModel = strings.TrimSpace(configResp.SmallModel)
	snapshot.ProviderKeys = sortedMapKeys(configResp.Provider)
	snapshot.RawConfigJSON = configResp.JSON.RawJSON()
	snapshot.RawProvidersJSON = providersResp.JSON.RawJSON()
	snapshot.ConfigExtraFieldKeys = extraFieldKeys(configResp.JSON.ExtraFields)
	for key, value := range providersResp.Default {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		snapshot.DefaultProviderModels[trimmedKey] = trimmedValue
	}
	for _, provider := range providersResp.Providers {
		providerSnapshot := RuntimeProviderSnapshot{
			ID:             strings.TrimSpace(provider.ID),
			Name:           strings.TrimSpace(provider.Name),
			API:            strings.TrimSpace(provider.API),
			NPM:            strings.TrimSpace(provider.Npm),
			Env:            sortedStrings(provider.Env),
			ExtraFieldKeys: extraFieldKeys(provider.JSON.ExtraFields),
			RawJSON:        provider.JSON.RawJSON(),
		}
		for _, modelID := range sortedMapKeys(provider.Models) {
			model := provider.Models[modelID]
			modelSnapshot := RuntimeModelSnapshot{
				ID:               strings.TrimSpace(model.ID),
				Name:             strings.TrimSpace(model.Name),
				ProviderID:       strings.TrimSpace(provider.ID),
				ProviderNPM:      strings.TrimSpace(model.Provider.Npm),
				RawJSON:          model.JSON.RawJSON(),
				ExtraFieldKeys:   extraFieldKeys(model.JSON.ExtraFields),
				OptionKeys:       sortedMapKeys(model.Options),
				Experimental:     model.Experimental,
				Reasoning:        model.Reasoning,
				ToolCall:         model.ToolCall,
				Temperature:      model.Temperature,
				Attachment:       model.Attachment,
				ContextLimit:     int64(model.Limit.Context),
				OutputLimit:      int64(model.Limit.Output),
				ReleaseDate:      strings.TrimSpace(model.ReleaseDate),
				Status:           strings.TrimSpace(string(model.Status)),
				InputModalities:  modalitiesToStrings(model.Modalities.Input),
				OutputModalities: modalitiesToStrings(model.Modalities.Output),
			}
			providerSnapshot.ModelIDs = append(providerSnapshot.ModelIDs, modelSnapshot.ID)
			providerSnapshot.Models = append(providerSnapshot.Models, modelSnapshot)
		}
		sort.Strings(providerSnapshot.ModelIDs)
		snapshot.ProviderExtraFieldMap[providerSnapshot.ID] = append([]string(nil), providerSnapshot.ExtraFieldKeys...)
		snapshot.Providers = append(snapshot.Providers, providerSnapshot)
	}
	sort.Slice(snapshot.Providers, func(i, j int) bool { return snapshot.Providers[i].ID < snapshot.Providers[j].ID })
	return snapshot
}

func runtimeRequestOptions(directory string) []option.RequestOption {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil
	}
	return []option.RequestOption{option.WithQuery("directory", directory)}
}

func snapshotFileProvider(protocol string, providerKey string, providerEntry map[string]any) FileProviderSnapshot {
	snapshot := FileProviderSnapshot{Key: providerKey, Protocol: strings.TrimSpace(protocol)}
	if providerEntry == nil {
		snapshot.MissingFields = []string{"provider." + providerKey}
		return snapshot
	}
	snapshot.ContractConfigured = true
	snapshot.Name, _ = providerEntry["name"].(string)
	snapshot.NPM, _ = providerEntry["npm"].(string)
	if opts, _ := providerEntry["options"].(map[string]any); opts != nil {
		snapshot.BaseURL, _ = opts["baseURL"].(string)
		snapshot.UnknownFieldKeys = append(snapshot.UnknownFieldKeys, unknownKeys(opts, map[string]bool{"baseURL": true, "apiKey": true, "setCacheKey": true, "headers": true})...)
	}
	if models, _ := providerEntry["models"].(map[string]any); models != nil {
		for alias := range models {
			snapshot.ModelAliases = append(snapshot.ModelAliases, alias)
		}
		sort.Strings(snapshot.ModelAliases)
	} else {
		snapshot.MissingFields = append(snapshot.MissingFields, "provider."+providerKey+".models")
	}
	if _, ok := providerEntry["options"].(map[string]any); !ok {
		snapshot.MissingFields = append(snapshot.MissingFields, "provider."+providerKey+".options")
	}
	if snapshot.Name == "" {
		snapshot.MissingFields = append(snapshot.MissingFields, "provider."+providerKey+".name")
	}
	if snapshot.NPM == "" {
		snapshot.MissingFields = append(snapshot.MissingFields, "provider."+providerKey+".npm")
	}
	return snapshot
}

func applyRuntimeError(snapshot *RuntimeConfigSnapshot, err error) {
	snapshot.ErrorMessage = err.Error()
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		snapshot.Reachable = true
		snapshot.HTTPStatus = apiErr.StatusCode
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			snapshot.ErrorCode = "runtime_auth_failed"
		default:
			snapshot.ErrorCode = "runtime_bad_status"
		}
		return
	}
	if isParseError(err) {
		snapshot.ErrorCode = "runtime_parse_error"
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		snapshot.ErrorCode = "runtime_unreachable"
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		snapshot.ErrorCode = "runtime_unreachable"
		return
	}
	snapshot.ErrorCode = "runtime_unreachable"
}

func modalitiesToStrings[T ~string](items []T) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(string(item))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func extraFieldKeys[T any](fields map[string]T) []string {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func unknownKeys(raw map[string]any, known map[string]bool) []string {
	if len(raw) == 0 {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		if !known[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys[V any](items map[string]V) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func isParseError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "json") || strings.Contains(msg, "decode") || strings.Contains(msg, "unmarshal")
}

func syncedProviderKey(protocol string) string {
	if strings.TrimSpace(protocol) == "anthropic-messages" {
		return AnthropicProviderKey
	}
	return ProviderKey
}

func DescribeRuntimeError(snapshot RuntimeConfigSnapshot) error {
	if strings.TrimSpace(snapshot.ErrorCode) == "" {
		return nil
	}
	if snapshot.HTTPStatus > 0 {
		return fmt.Errorf("%s: %s (status %d)", snapshot.ErrorCode, snapshot.ErrorMessage, snapshot.HTTPStatus)
	}
	return fmt.Errorf("%s: %s", snapshot.ErrorCode, snapshot.ErrorMessage)
}
