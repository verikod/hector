// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel
//
// Licensed under the GNU Affero General Public License v3.0 (AGPL-3.0) (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.gnu.org/licenses/agpl-3.0.en.html
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// stringToUUID generates a deterministic UUID v5 from a string ID.
// This is necessary because Qdrant requires UUID format for point IDs,
// but Hector uses file paths and document IDs as string identifiers.
// The original ID is stored in metadata under "_original_id" for retrieval.
func stringToUUID(id string) string {
	// Use SHA256 hash to generate deterministic UUID
	hash := sha256.Sum256([]byte(id))
	// Create UUID from first 16 bytes of hash
	return uuid.UUID(hash[:16]).String()
}

// QdrantConfig configures the Qdrant vector provider.
//
// Direct port from legacy pkg/databases/qdrant.go
type QdrantConfig struct {
	// Host is the Qdrant server hostname.
	Host string `yaml:"host"`

	// Port is the Qdrant gRPC port (default: 6334).
	Port int `yaml:"port"`

	// APIKey for authenticated access (optional).
	APIKey string `yaml:"api_key,omitempty"`

	// UseTLS enables TLS connections.
	UseTLS bool `yaml:"use_tls,omitempty"`
}

// QdrantProvider implements Provider using Qdrant vector database.
//
// Direct port from legacy pkg/databases/qdrant.go
type QdrantProvider struct {
	client *qdrant.Client
	config QdrantConfig
}

// NewQdrantProvider creates a new Qdrant provider.
func NewQdrantProvider(cfg QdrantConfig) (*QdrantProvider, error) {
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 6334 // Qdrant gRPC port
	}

	client, err := qdrant.NewClient(&qdrant.Config{
		Host:   cfg.Host,
		Port:   cfg.Port,
		APIKey: cfg.APIKey,
		UseTLS: cfg.UseTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Qdrant client for %s:%d: %w\n"+
			"  TIP: Troubleshooting:\n"+
			"     - Ensure Qdrant is running\n"+
			"     - Verify host and port configuration\n"+
			"     - For Docker: start Qdrant container (docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant)",
			cfg.Host, cfg.Port, err)
	}

	return &QdrantProvider{
		client: client,
		config: cfg,
	}, nil
}

// Name returns the provider name.
func (p *QdrantProvider) Name() string {
	return "qdrant"
}

// Upsert adds or updates a document with its vector.
func (p *QdrantProvider) Upsert(ctx context.Context, collection string, id string, vector []float32, metadata map[string]any) error {
	// Check if collection exists, create if not
	exists, err := p.client.CollectionExists(ctx, collection)
	if err != nil {
		return fmt.Errorf("failed to check collection existence: %w", err)
	}

	if !exists {
		err = p.client.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: collection,
			VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
				Size:     uint64(len(vector)),
				Distance: qdrant.Distance_Cosine,
			}),
		})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create collection: %w", err)
		}
	}

	// Convert metadata to Qdrant payload
	payload := make(map[string]*qdrant.Value)
	for key, value := range metadata {
		val, err := qdrant.NewValue(value)
		if err != nil {
			return fmt.Errorf("failed to convert metadata value for key %s: %w", key, err)
		}
		payload[key] = val
	}

	// Store original ID in payload for retrieval (Qdrant requires UUID format for point IDs)
	originalIDVal, _ := qdrant.NewValue(id)
	payload["_original_id"] = originalIDVal

	// Convert string ID to deterministic UUID for Qdrant
	uuidID := stringToUUID(id)

	point := &qdrant.PointStruct{
		Id:      qdrant.NewID(uuidID),
		Vectors: qdrant.NewVectors(vector...),
		Payload: payload,
	}

	_, err = p.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         []*qdrant.PointStruct{point},
	})
	if err != nil {
		return fmt.Errorf("failed to upsert point: %w", err)
	}

	return nil
}

// Search finds the most similar vectors.
func (p *QdrantProvider) Search(ctx context.Context, collection string, vector []float32, topK int) ([]Result, error) {
	return p.SearchWithFilter(ctx, collection, vector, topK, nil)
}

// SearchWithFilter combines vector similarity with metadata filtering.
func (p *QdrantProvider) SearchWithFilter(ctx context.Context, collection string, vector []float32, topK int, filter map[string]any) ([]Result, error) {
	searchRequest := &qdrant.SearchPoints{
		CollectionName: collection,
		Vector:         vector,
		Limit:          uint64(topK),
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(true),
	}

	if len(filter) > 0 {
		searchRequest.Filter = buildQdrantFilter(filter)
	}

	pointsClient := p.client.GetPointsClient()
	searchResult, err := pointsClient.Search(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search points: %w", err)
	}

	return convertQdrantResults(searchResult.Result), nil
}

// Delete removes a document by ID.
func (p *QdrantProvider) Delete(ctx context.Context, collection string, id string) error {
	// Convert string ID to deterministic UUID for Qdrant
	uuidID := stringToUUID(id)

	deletePoints := &qdrant.DeletePoints{
		CollectionName: collection,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{
					Ids: []*qdrant.PointId{
						{PointIdOptions: &qdrant.PointId_Uuid{Uuid: uuidID}},
					},
				},
			},
		},
	}
	_, err := p.client.Delete(ctx, deletePoints)
	if err != nil {
		return fmt.Errorf("failed to delete point %s: %w", id, err)
	}
	return nil
}

// DeleteByFilter removes all documents matching the filter.
func (p *QdrantProvider) DeleteByFilter(ctx context.Context, collection string, filter map[string]any) error {
	qdrantFilter := buildQdrantFilter(filter)

	deletePoints := &qdrant.DeletePoints{
		CollectionName: collection,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: qdrantFilter,
			},
		},
	}

	_, err := p.client.Delete(ctx, deletePoints)
	if err != nil {
		return fmt.Errorf("failed to delete by filter: %w", err)
	}
	return nil
}

// CreateCollection creates a new collection.
func (p *QdrantProvider) CreateCollection(ctx context.Context, collection string, vectorDimension int) error {
	exists, err := p.client.CollectionExists(ctx, collection)
	if err != nil {
		return fmt.Errorf("failed to check collection: %w", err)
	}

	if exists {
		return nil
	}

	err = p.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(vectorDimension),
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}
	return nil
}

// DeleteCollection removes a collection.
func (p *QdrantProvider) DeleteCollection(ctx context.Context, collection string) error {
	err := p.client.DeleteCollection(ctx, collection)
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}
	return nil
}

// Close closes the Qdrant client.
func (p *QdrantProvider) Close() error {
	return p.client.Close()
}

// buildQdrantFilter converts a filter map to Qdrant filter.
func buildQdrantFilter(filter map[string]any) *qdrant.Filter {
	conditions := make([]*qdrant.Condition, 0, len(filter))

	for key, value := range filter {
		val, err := qdrant.NewValue(value)
		if err != nil {
			continue
		}

		condition := &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key: key,
					Match: &qdrant.Match{
						MatchValue: &qdrant.Match_Keyword{
							Keyword: val.GetStringValue(),
						},
					},
				},
			},
		}
		conditions = append(conditions, condition)
	}

	return &qdrant.Filter{
		Must: conditions,
	}
}

// convertQdrantResults converts Qdrant results to our Result type.
func convertQdrantResults(points []*qdrant.ScoredPoint) []Result {
	results := make([]Result, 0, len(points))

	for _, point := range points {
		var id string
		if point.Id != nil && point.Id.PointIdOptions != nil {
			switch idType := point.Id.PointIdOptions.(type) {
			case *qdrant.PointId_Uuid:
				id = idType.Uuid
			case *qdrant.PointId_Num:
				id = fmt.Sprintf("%d", idType.Num)
			}
		}

		var vector []float32
		if point.Vectors != nil {
			if vectorData := point.Vectors.GetVector(); vectorData != nil {
				switch v := vectorData.Vector.(type) {
				case *qdrant.VectorOutput_Dense:
					if v.Dense != nil {
						vector = v.Dense.Data
					}
				}
			}
		}

		metadata := make(map[string]any)
		if point.Payload != nil {
			for key, value := range point.Payload {
				switch v := value.Kind.(type) {
				case *qdrant.Value_StringValue:
					metadata[key] = v.StringValue
				case *qdrant.Value_IntegerValue:
					metadata[key] = v.IntegerValue
				case *qdrant.Value_DoubleValue:
					metadata[key] = v.DoubleValue
				case *qdrant.Value_BoolValue:
					metadata[key] = v.BoolValue
				case *qdrant.Value_ListValue:
					if v.ListValue != nil {
						list := make([]any, len(v.ListValue.Values))
						for i, item := range v.ListValue.Values {
							switch itemVal := item.Kind.(type) {
							case *qdrant.Value_StringValue:
								list[i] = itemVal.StringValue
							case *qdrant.Value_IntegerValue:
								list[i] = itemVal.IntegerValue
							case *qdrant.Value_DoubleValue:
								list[i] = itemVal.DoubleValue
							case *qdrant.Value_BoolValue:
								list[i] = itemVal.BoolValue
							default:
								list[i] = item
							}
						}
						metadata[key] = list
					}
				default:
					metadata[key] = value
				}
			}
		}

		content := ""
		if contentValue, exists := metadata["content"]; exists {
			if contentStr, ok := contentValue.(string); ok {
				content = contentStr
			}
		}

		// Extract original ID from metadata (we stored it during Upsert)
		// Fall back to UUID if _original_id is not present (legacy data)
		originalID := id
		if origIDValue, exists := metadata["_original_id"]; exists {
			if origIDStr, ok := origIDValue.(string); ok {
				originalID = origIDStr
			}
			// Remove internal field from returned metadata
			delete(metadata, "_original_id")
		}

		results = append(results, Result{
			ID:       originalID,
			Content:  content,
			Vector:   vector,
			Metadata: metadata,
			Score:    point.Score,
		})
	}

	return results
}

// Ensure QdrantProvider implements Provider.
var _ Provider = (*QdrantProvider)(nil)
