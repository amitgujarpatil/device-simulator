package mongoclient

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ── Types ──────────────────────────────────────────────────────────────────

type CollectionMeta struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type CollStats struct {
	Count       int64   `json:"count"`
	StorageSize int64   `json:"storageSize"`
	AvgDocSize  float64 `json:"avgDocSize"`
}

type FindResult struct {
	Docs  []json.RawMessage `json:"docs"`
	Total int64             `json:"total"`
	Skip  int               `json:"skip"`
	Limit int               `json:"limit"`
}

type Index struct {
	Name    string          `json:"name"`
	Keys    json.RawMessage `json:"keys"`
	Unique  bool            `json:"unique"`
	Sparse  bool            `json:"sparse"`
}

type FieldStat struct {
	Path      string  `json:"path"`
	Type      string  `json:"type"`
	Frequency float64 `json:"frequency"`
	NullPct   float64 `json:"nullPct"`
}

type ImportResult struct {
	Inserted int      `json:"inserted"`
	Updated  int      `json:"updated"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors"`
}

// ── Pool ───────────────────────────────────────────────────────────────────

var (
	mu      sync.RWMutex
	clients = map[string]*mongo.Client{}
)

func dial(uri string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := options.Client().ApplyURI(uri).
		SetServerSelectionTimeout(8 * time.Second).
		SetConnectTimeout(8 * time.Second)
	c, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := c.Ping(ctx, nil); err != nil {
		c.Disconnect(context.Background())
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	return c, nil
}

func Connect(id, uri string) error {
	mu.Lock()
	defer mu.Unlock()
	if old, ok := clients[id]; ok {
		old.Disconnect(context.Background())
		delete(clients, id)
	}
	c, err := dial(uri)
	if err != nil {
		return err
	}
	clients[id] = c
	return nil
}

func Disconnect(id string) {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := clients[id]; ok {
		c.Disconnect(context.Background())
		delete(clients, id)
	}
}

func get(id string) (*mongo.Client, error) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := clients[id]
	if !ok {
		return nil, fmt.Errorf("not connected (id=%s)", id)
	}
	return c, nil
}

func ctx10() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ── Databases / Collections ────────────────────────────────────────────────

func ListDatabases(id string) ([]string, error) {
	c, err := get(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	return c.ListDatabaseNames(ctx, bson.D{})
}

func ListCollections(id, db string) ([]CollectionMeta, error) {
	c, err := get(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	names, err := c.Database(db).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]CollectionMeta, 0, len(names))
	for _, name := range names {
		count, _ := c.Database(db).Collection(name).EstimatedDocumentCount(ctx)
		out = append(out, CollectionMeta{Name: name, Count: count})
	}
	return out, nil
}

func GetCollStats(id, db, coll string) (CollStats, error) {
	c, err := get(id)
	if err != nil {
		return CollStats{}, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	var result bson.M
	if err := c.Database(db).RunCommand(ctx, bson.D{{Key: "collStats", Value: coll}}).Decode(&result); err != nil {
		return CollStats{}, err
	}
	s := CollStats{}
	if v, ok := result["count"]; ok {
		s.Count, _ = toInt64(v)
	}
	if v, ok := result["storageSize"]; ok {
		s.StorageSize, _ = toInt64(v)
	}
	if v, ok := result["avgObjSize"]; ok {
		s.AvgDocSize, _ = toFloat64(v)
	}
	return s, nil
}

// ── Find ──────────────────────────────────────────────────────────────────

func Find(id, db, coll, filterJSON, sortJSON, projJSON string, skip, limit int) (FindResult, error) {
	c, err := get(id)
	if err != nil {
		return FindResult{}, err
	}
	filter, err := parseFilter(filterJSON)
	if err != nil {
		return FindResult{}, fmt.Errorf("filter: %w", err)
	}
	ctx, cancel := ctx10()
	defer cancel()

	if limit <= 0 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}

	findOpts := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	if sortJSON != "" && sortJSON != "{}" {
		var s bson.D
		if err := bson.UnmarshalExtJSON([]byte(sortJSON), true, &s); err == nil {
			findOpts.SetSort(s)
		}
	}
	if projJSON != "" && projJSON != "{}" {
		var p bson.D
		if err := bson.UnmarshalExtJSON([]byte(projJSON), true, &p); err == nil {
			findOpts.SetProjection(p)
		}
	}

	total, _ := c.Database(db).Collection(coll).CountDocuments(ctx, filter)
	cursor, err := c.Database(db).Collection(coll).Find(ctx, filter, findOpts)
	if err != nil {
		return FindResult{}, err
	}
	defer cursor.Close(ctx)

	var docs []json.RawMessage
	for cursor.Next(ctx) {
		raw := cursor.Current
		// Convert BSON to canonical extended JSON
		b, err := bson.MarshalExtJSON(raw, false, false)
		if err != nil {
			continue
		}
		docs = append(docs, json.RawMessage(b))
	}
	if docs == nil {
		docs = []json.RawMessage{}
	}
	return FindResult{Docs: docs, Total: total, Skip: skip, Limit: limit}, nil
}

// ── CRUD ──────────────────────────────────────────────────────────────────

func InsertOne(id, db, coll, docJSON string) (string, error) {
	c, err := get(id)
	if err != nil {
		return "", err
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), false, &doc); err != nil {
		return "", fmt.Errorf("parse document: %w", err)
	}
	ctx, cancel := ctx10()
	defer cancel()
	res, err := c.Database(db).Collection(coll).InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}
	switch v := res.InsertedID.(type) {
	case primitive.ObjectID:
		return v.Hex(), nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

func UpdateOne(id, db, coll, filterJSON, updateJSON string) (int64, error) {
	c, err := get(id)
	if err != nil {
		return 0, err
	}
	filter, err := parseFilter(filterJSON)
	if err != nil {
		return 0, fmt.Errorf("filter: %w", err)
	}
	var update bson.D
	if err := bson.UnmarshalExtJSON([]byte(updateJSON), false, &update); err != nil {
		return 0, fmt.Errorf("update: %w", err)
	}
	ctx, cancel := ctx10()
	defer cancel()
	res, err := c.Database(db).Collection(coll).UpdateOne(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return res.MatchedCount, nil
}

func DeleteOne(id, db, coll, filterJSON string) (int64, error) {
	c, err := get(id)
	if err != nil {
		return 0, err
	}
	filter, err := parseFilter(filterJSON)
	if err != nil {
		return 0, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	res, err := c.Database(db).Collection(coll).DeleteOne(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func DeleteMany(id, db, coll, filterJSON string) (int64, error) {
	c, err := get(id)
	if err != nil {
		return 0, err
	}
	filter, err := parseFilter(filterJSON)
	if err != nil {
		return 0, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	res, err := c.Database(db).Collection(coll).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// ── Aggregate ─────────────────────────────────────────────────────────────

func Aggregate(id, db, coll, pipelineJSON string, limit int) (FindResult, error) {
	c, err := get(id)
	if err != nil {
		return FindResult{}, err
	}
	var pipeline []bson.D
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &pipeline); err != nil {
		return FindResult{}, fmt.Errorf("parse pipeline: %w", err)
	}
	if limit > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit}})
	}
	ctx, cancel := ctx10()
	defer cancel()
	cursor, err := c.Database(db).Collection(coll).Aggregate(ctx, pipeline)
	if err != nil {
		return FindResult{}, err
	}
	defer cursor.Close(ctx)
	var docs []json.RawMessage
	for cursor.Next(ctx) {
		b, err := bson.MarshalExtJSON(cursor.Current, false, false)
		if err != nil {
			continue
		}
		docs = append(docs, json.RawMessage(b))
	}
	if docs == nil {
		docs = []json.RawMessage{}
	}
	return FindResult{Docs: docs, Total: int64(len(docs)), Skip: 0, Limit: limit}, nil
}

// ── Indexes ───────────────────────────────────────────────────────────────

func ListIndexes(id, db, coll string) ([]Index, error) {
	c, err := get(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctx10()
	defer cancel()
	cursor, err := c.Database(db).Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []Index
	for cursor.Next(ctx) {
		var raw bson.M
		if err := cursor.Decode(&raw); err != nil {
			continue
		}
		idx := Index{}
		if v, ok := raw["name"].(string); ok {
			idx.Name = v
		}
		if v, ok := raw["unique"].(bool); ok {
			idx.Unique = v
		}
		if v, ok := raw["sparse"].(bool); ok {
			idx.Sparse = v
		}
		if key, ok := raw["key"]; ok {
			b, _ := bson.MarshalExtJSON(key.(bson.M), false, false)
			idx.Keys = json.RawMessage(b)
		}
		out = append(out, idx)
	}
	if out == nil {
		out = []Index{}
	}
	return out, nil
}

func CreateIndex(id, db, coll, keysJSON, optionsJSON string) (string, error) {
	c, err := get(id)
	if err != nil {
		return "", err
	}
	var keys bson.D
	if err := bson.UnmarshalExtJSON([]byte(keysJSON), true, &keys); err != nil {
		return "", fmt.Errorf("parse keys: %w", err)
	}
	idxModel := mongo.IndexModel{Keys: keys}
	if optionsJSON != "" && optionsJSON != "{}" {
		var opts bson.M
		if err := bson.UnmarshalExtJSON([]byte(optionsJSON), true, &opts); err == nil {
			idxOpts := options.Index()
			if v, ok := opts["unique"].(bool); ok && v {
				idxOpts.SetUnique(true)
			}
			if v, ok := opts["sparse"].(bool); ok && v {
				idxOpts.SetSparse(true)
			}
			if v, ok := opts["name"].(string); ok && v != "" {
				idxOpts.SetName(v)
			}
			idxModel.Options = idxOpts
		}
	}
	ctx, cancel := ctx10()
	defer cancel()
	name, err := c.Database(db).Collection(coll).Indexes().CreateOne(ctx, idxModel)
	return name, err
}

func DropIndex(id, db, coll, indexName string) error {
	c, err := get(id)
	if err != nil {
		return err
	}
	ctx, cancel := ctx10()
	defer cancel()
	_, err = c.Database(db).Collection(coll).Indexes().DropOne(ctx, indexName)
	return err
}

// ── Schema analysis ────────────────────────────────────────────────────────

func SchemaAnalyze(id, db, coll, filterJSON string, sampleSize int) ([]FieldStat, error) {
	c, err := get(id)
	if err != nil {
		return nil, err
	}
	filter, err := parseFilter(filterJSON)
	if err != nil {
		return nil, err
	}
	if sampleSize <= 0 {
		sampleSize = 500
	}
	ctx, cancel := ctx10()
	defer cancel()
	opts := options.Find().SetLimit(int64(sampleSize))
	cursor, err := c.Database(db).Collection(coll).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	fieldCounts := map[string]int{}
	fieldTypes := map[string]map[string]int{}
	nullCounts := map[string]int{}
	total := 0

	for cursor.Next(ctx) {
		total++
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		walkDoc("", doc, fieldCounts, fieldTypes, nullCounts)
	}

	if total == 0 {
		return []FieldStat{}, nil
	}

	stats := make([]FieldStat, 0, len(fieldCounts))
	for path, count := range fieldCounts {
		freq := float64(count) / float64(total)
		nullPct := 0.0
		if nc, ok := nullCounts[path]; ok {
			nullPct = float64(nc) / float64(total)
		}
		typeName := ""
		if tm, ok := fieldTypes[path]; ok {
			typeName = dominantType(tm)
		}
		stats = append(stats, FieldStat{
			Path:      path,
			Type:      typeName,
			Frequency: freq,
			NullPct:   nullPct,
		})
	}
	// sort by frequency desc (insertion sort)
	for i := 1; i < len(stats); i++ {
		for j := i; j > 0 && stats[j].Frequency > stats[j-1].Frequency; j-- {
			stats[j], stats[j-1] = stats[j-1], stats[j]
		}
	}
	return stats, nil
}

func walkDoc(prefix string, doc bson.M, counts map[string]int, types map[string]map[string]int, nulls map[string]int) {
	for k, v := range doc {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		counts[path]++
		typeName := bsonTypeName(v)
		if types[path] == nil {
			types[path] = map[string]int{}
		}
		types[path][typeName]++
		if v == nil {
			nulls[path]++
		}
		if sub, ok := v.(bson.M); ok {
			walkDoc(path, sub, counts, types, nulls)
		}
	}
}

func bsonTypeName(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case int32, int64, float64:
		return "number"
	case bool:
		return "bool"
	case primitive.ObjectID:
		return "ObjectId"
	case primitive.DateTime:
		return "date"
	case bson.M, bson.D:
		return "object"
	case bson.A:
		return "array"
	default:
		return "other"
	}
}

func dominantType(tm map[string]int) string {
	best, bestN := "", 0
	for t, n := range tm {
		if n > bestN {
			best, bestN = t, n
		}
	}
	return best
}

// ── Export ────────────────────────────────────────────────────────────────

func Export(id, db, coll, filterJSON, format string) (string, error) {
	result, err := Find(id, db, coll, filterJSON, "", "", 0, 10000)
	if err != nil {
		return "", err
	}
	switch format {
	case "jsonl":
		var sb strings.Builder
		for _, d := range result.Docs {
			sb.Write(d)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	case "csv":
		return docsToCSV(result.Docs)
	default: // json
		b, err := json.MarshalIndent(result.Docs, "", "  ")
		return string(b), err
	}
}

func docsToCSV(docs []json.RawMessage) (string, error) {
	if len(docs) == 0 {
		return "", nil
	}
	// collect keys from first 20 docs
	keyOrder := []string{}
	keySet := map[string]bool{}
	for i, d := range docs {
		if i >= 20 {
			break
		}
		var m map[string]interface{}
		if err := json.Unmarshal(d, &m); err != nil {
			continue
		}
		for k := range m {
			if !keySet[k] {
				keySet[k] = true
				keyOrder = append(keyOrder, k)
			}
		}
	}
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	w.Write(keyOrder)
	for _, d := range docs {
		var m map[string]interface{}
		if err := json.Unmarshal(d, &m); err != nil {
			continue
		}
		row := make([]string, len(keyOrder))
		for i, k := range keyOrder {
			if v, ok := m[k]; ok {
				switch tv := v.(type) {
				case string:
					row[i] = tv
				default:
					b, _ := json.Marshal(tv)
					row[i] = string(b)
				}
			}
		}
		w.Write(row)
	}
	w.Flush()
	return sb.String(), nil
}

// ── Import ────────────────────────────────────────────────────────────────

func Import(id, db, coll, format, filePath string, upsert bool) (ImportResult, error) {
	c, err := get(id)
	if err != nil {
		return ImportResult{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ImportResult{}, err
	}
	var docs []bson.D
	switch format {
	case "jsonl":
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var d bson.D
			if err := bson.UnmarshalExtJSON([]byte(line), false, &d); err != nil {
				continue
			}
			docs = append(docs, d)
		}
	case "csv":
		r := csv.NewReader(strings.NewReader(string(data)))
		records, err := r.ReadAll()
		if err != nil {
			return ImportResult{}, err
		}
		if len(records) < 2 {
			return ImportResult{Inserted: 0}, nil
		}
		headers := records[0]
		for _, row := range records[1:] {
			d := bson.D{}
			for i, h := range headers {
				if i < len(row) {
					d = append(d, bson.E{Key: h, Value: row[i]})
				}
			}
			docs = append(docs, d)
		}
	default: // json array
		var arr []bson.D
		if err := bson.UnmarshalExtJSON(data, false, &arr); err != nil {
			return ImportResult{}, fmt.Errorf("parse JSON: %w", err)
		}
		docs = arr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := ImportResult{}
	for _, doc := range docs {
		if upsert {
			// find _id and upsert
			var filterID interface{}
			for _, e := range doc {
				if e.Key == "_id" {
					filterID = e.Value
					break
				}
			}
			if filterID != nil {
				opts := options.Replace().SetUpsert(true)
				res, err := c.Database(db).Collection(coll).ReplaceOne(ctx, bson.D{{Key: "_id", Value: filterID}}, doc, opts)
				if err != nil {
					result.Failed++
					result.Errors = append(result.Errors, err.Error())
					continue
				}
				if res.UpsertedCount > 0 {
					result.Inserted++
				} else {
					result.Updated++
				}
				continue
			}
		}
		_, err := c.Database(db).Collection(coll).InsertOne(ctx, doc)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, err.Error())
		} else {
			result.Inserted++
		}
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

func parseFilter(filterJSON string) (bson.D, error) {
	if filterJSON == "" || filterJSON == "{}" {
		return bson.D{}, nil
	}
	var f bson.D
	if err := bson.UnmarshalExtJSON([]byte(filterJSON), true, &f); err != nil {
		return nil, err
	}
	return f, nil
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	}
	return 0, false
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
