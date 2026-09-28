package mongoclient

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RawResult is the return type for RunRaw.
type RawResult struct {
	Docs    []json.RawMessage `json:"docs"`
	Count   int64             `json:"count"`
	Message string            `json:"message"`
}

// ── Collection management ──────────────────────────────────────────────────

func CreateCollection(id, db, coll string) error {
	c, err := get(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.Database(db).CreateCollection(ctx, coll)
}

func DropCollection(id, db, coll string) error {
	c, err := get(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.Database(db).Collection(coll).Drop(ctx)
}

func RenameCollection(id, db, coll, newName string) error {
	c, err := get(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res := c.Database("admin").RunCommand(ctx, bson.D{
		{Key: "renameCollection", Value: db + "." + coll},
		{Key: "to", Value: db + "." + newName},
		{Key: "dropTarget", Value: false},
	})
	return res.Err()
}

// ── Raw query ──────────────────────────────────────────────────────────────

var (
	reCreateColl  = regexp.MustCompile(`(?i)^db\.createCollection\s*\(\s*["']([^"']+)["']\s*\)$`)
	reCollOp      = regexp.MustCompile(`^db\.([a-zA-Z0-9_$]+)\.([a-zA-Z]+)\s*\(`)
	reObjectId    = regexp.MustCompile(`ObjectId\s*\(\s*["']([a-fA-F0-9]{24})["']\s*\)`)
	reISODate     = regexp.MustCompile(`(?:ISODate|new\s+Date)\s*\(\s*["']([^"']+)["']\s*\)`)
	reNumberInt   = regexp.MustCompile(`NumberInt\s*\(\s*(\d+)\s*\)`)
	reNumberLong  = regexp.MustCompile(`NumberLong\s*\(\s*(\d+)\s*\)`)
)

// RunRaw parses and executes a MongoDB shell-style query.
func RunRaw(connID, database, rawQuery string) (RawResult, error) {
	q := strings.TrimSpace(rawQuery)
	q = strings.TrimSuffix(q, ";")
	q = strings.TrimSpace(q)

	lower := strings.ToLower(q)

	// show commands
	if lower == "show collections" || lower == "show collection" {
		colls, err := ListCollections(connID, database)
		if err != nil {
			return RawResult{}, err
		}
		docs := make([]json.RawMessage, len(colls))
		for i, c := range colls {
			b, _ := json.Marshal(map[string]interface{}{"name": c.Name, "count": c.Count})
			docs[i] = json.RawMessage(b)
		}
		return RawResult{Docs: docs, Count: int64(len(docs)), Message: fmt.Sprintf("%d collections", len(docs))}, nil
	}
	if lower == "show dbs" || lower == "show databases" {
		dbs, err := ListDatabases(connID)
		if err != nil {
			return RawResult{}, err
		}
		docs := make([]json.RawMessage, len(dbs))
		for i, d := range dbs {
			b, _ := json.Marshal(map[string]string{"name": d})
			docs[i] = json.RawMessage(b)
		}
		return RawResult{Docs: docs, Count: int64(len(docs))}, nil
	}

	// preprocess shell syntax → extended JSON
	q = preprocessShell(q)

	// db.createCollection("name")
	if m := reCreateColl.FindStringSubmatch(q); m != nil {
		if err := CreateCollection(connID, database, m[1]); err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"ok": 1, "collection": m[1]})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Message: fmt.Sprintf("Collection '%s' created", m[1])}, nil
	}

	if !strings.HasPrefix(strings.ToLower(q), "db.") {
		return RawResult{}, fmt.Errorf("syntax: db.collection.operation(...) | db.createCollection(...) | show collections | show dbs")
	}

	// Extract collection name
	after := q[3:] // strip "db."
	dotIdx := strings.IndexByte(after, '.')
	if dotIdx < 0 {
		return RawResult{}, fmt.Errorf("syntax: db.collection.operation(...)")
	}
	collName := after[:dotIdx]
	after = after[dotIdx+1:]

	// Extract operation name (up to first '(')
	parenIdx := strings.IndexByte(after, '(')
	if parenIdx < 0 {
		return RawResult{}, fmt.Errorf("missing '(' — syntax: db.%s.operation(...)", collName)
	}
	opName := strings.ToLower(strings.TrimSpace(after[:parenIdx]))
	argsRaw := after[parenIdx+1:]

	// Extract balanced content inside the outer parens
	argsStr, err := extractBetweenParens(argsRaw)
	if err != nil {
		return RawResult{}, err
	}

	return executeOp(connID, database, collName, opName, strings.TrimSpace(argsStr))
}

// preprocessShell converts shell shorthand to extended-JSON.
func preprocessShell(s string) string {
	s = reObjectId.ReplaceAllString(s, `{"$$oid":"${1}"}`)
	s = reISODate.ReplaceAllString(s, `{"$$date":"${1}"}`)
	s = reNumberInt.ReplaceAllStringFunc(s, func(m string) string {
		sub := reNumberInt.FindStringSubmatch(m)
		if len(sub) > 1 { return sub[1] }
		return m
	})
	s = reNumberLong.ReplaceAllStringFunc(s, func(m string) string {
		sub := reNumberLong.FindStringSubmatch(m)
		if len(sub) > 1 { return sub[1] }
		return m
	})
	return s
}

// extractBetweenParens extracts the string up to the matching ')'.
// s starts right after an opening '('.
func extractBetweenParens(s string) (string, error) {
	depth := 1
	inStr := false
	strChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == strChar && (i == 0 || s[i-1] != '\\') {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = true
			strChar = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return s[:i], nil
			}
		}
	}
	return strings.TrimRight(s, ")"), nil // gracefully handle missing closing paren
}

// splitArgs splits a string at top-level commas.
func splitArgs(s string) []string {
	var args []string
	depth := 0
	inStr := false
	strChar := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == strChar && (i == 0 || s[i-1] != '\\') {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = true
			strChar = c
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				arg := strings.TrimSpace(s[start:i])
				if arg != "" {
					args = append(args, arg)
				}
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		args = append(args, last)
	}
	return args
}

func getArg(args []string, i int) string {
	if i < len(args) {
		return strings.TrimSpace(args[i])
	}
	return ""
}

func parseBSONFilter(s string) (bson.D, error) {
	if s == "" || s == "{}" {
		return bson.D{}, nil
	}
	var f bson.D
	return f, bson.UnmarshalExtJSON([]byte(s), true, &f)
}

func cursorToDocs(ctx context.Context, cursor *mongo.Cursor) []json.RawMessage {
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
	return docs
}

func executeOp(connID, database, coll, op, argsStr string) (RawResult, error) {
	c, err := get(connID)
	if err != nil {
		return RawResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	col := c.Database(database).Collection(coll)
	args := splitArgs(argsStr)

	switch op {
	case "find":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		findOpts := options.Find().SetLimit(500)
		if p := getArg(args, 1); p != "" && p != "{}" {
			var proj bson.D
			if e := bson.UnmarshalExtJSON([]byte(p), true, &proj); e == nil {
				findOpts.SetProjection(proj)
			}
		}
		cursor, err := col.Find(ctx, filter, findOpts)
		if err != nil {
			return RawResult{}, err
		}
		docs := cursorToDocs(ctx, cursor)
		return RawResult{Docs: docs, Count: int64(len(docs))}, nil

	case "findone":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		var raw bson.Raw
		if err := col.FindOne(ctx, filter).Decode(&raw); err != nil {
			if strings.Contains(err.Error(), "no documents") {
				return RawResult{Docs: []json.RawMessage{}, Count: 0, Message: "no document found"}, nil
			}
			return RawResult{}, err
		}
		b, _ := bson.MarshalExtJSON(raw, false, false)
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: 1}, nil

	case "aggregate":
		if argsStr == "" {
			argsStr = "[]"
		}
		var pipeline []bson.D
		if err := bson.UnmarshalExtJSON([]byte(argsStr), true, &pipeline); err != nil {
			return RawResult{}, fmt.Errorf("pipeline: %w", err)
		}
		cursor, err := col.Aggregate(ctx, pipeline)
		if err != nil {
			return RawResult{}, err
		}
		docs := cursorToDocs(ctx, cursor)
		return RawResult{Docs: docs, Count: int64(len(docs))}, nil

	case "insertone":
		arg := getArg(args, 0)
		if arg == "" {
			return RawResult{}, fmt.Errorf("insertOne requires a document")
		}
		var doc bson.D
		if err := bson.UnmarshalExtJSON([]byte(arg), false, &doc); err != nil {
			return RawResult{}, fmt.Errorf("document: %w", err)
		}
		res, err := col.InsertOne(ctx, doc)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"acknowledged": true, "insertedId": fmt.Sprintf("%v", res.InsertedID)})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: 1, Message: "1 document inserted"}, nil

	case "insertmany":
		if argsStr == "" {
			return RawResult{}, fmt.Errorf("insertMany requires an array")
		}
		var docs []bson.D
		if err := bson.UnmarshalExtJSON([]byte(argsStr), false, &docs); err != nil {
			return RawResult{}, fmt.Errorf("documents: %w", err)
		}
		iface := make([]interface{}, len(docs))
		for i, d := range docs {
			iface[i] = d
		}
		res, err := col.InsertMany(ctx, iface)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"acknowledged": true, "insertedCount": len(res.InsertedIDs)})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: int64(len(res.InsertedIDs)), Message: fmt.Sprintf("%d documents inserted", len(res.InsertedIDs))}, nil

	case "updateone":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		updateStr := getArg(args, 1)
		if updateStr == "" {
			return RawResult{}, fmt.Errorf("updateOne requires an update argument")
		}
		var update bson.D
		if err := bson.UnmarshalExtJSON([]byte(updateStr), false, &update); err != nil {
			return RawResult{}, fmt.Errorf("update: %w", err)
		}
		res, err := col.UpdateOne(ctx, filter, update)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"matchedCount": res.MatchedCount, "modifiedCount": res.ModifiedCount, "upsertedCount": res.UpsertedCount})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: res.ModifiedCount,
			Message: fmt.Sprintf("matched: %d  modified: %d", res.MatchedCount, res.ModifiedCount)}, nil

	case "updatemany":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		updateStr := getArg(args, 1)
		var update bson.D
		if err := bson.UnmarshalExtJSON([]byte(updateStr), false, &update); err != nil {
			return RawResult{}, fmt.Errorf("update: %w", err)
		}
		res, err := col.UpdateMany(ctx, filter, update)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"matchedCount": res.MatchedCount, "modifiedCount": res.ModifiedCount})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: res.ModifiedCount,
			Message: fmt.Sprintf("matched: %d  modified: %d", res.MatchedCount, res.ModifiedCount)}, nil

	case "deleteone":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		res, err := col.DeleteOne(ctx, filter)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"acknowledged": true, "deletedCount": res.DeletedCount})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: res.DeletedCount,
			Message: fmt.Sprintf("%d document deleted", res.DeletedCount)}, nil

	case "deletemany":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		res, err := col.DeleteMany(ctx, filter)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"acknowledged": true, "deletedCount": res.DeletedCount})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: res.DeletedCount,
			Message: fmt.Sprintf("%d documents deleted", res.DeletedCount)}, nil

	case "countdocuments":
		filter, err := parseBSONFilter(getArg(args, 0))
		if err != nil {
			return RawResult{}, fmt.Errorf("filter: %w", err)
		}
		n, err := col.CountDocuments(ctx, filter)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"count": n})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: n, Message: fmt.Sprintf("count: %d", n)}, nil

	case "estimateddocumentcount":
		n, err := col.EstimatedDocumentCount(ctx)
		if err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"count": n})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Count: n}, nil

	case "drop":
		if err := col.Drop(ctx); err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"ok": 1})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Message: fmt.Sprintf("collection '%s' dropped", coll)}, nil

	case "renamecollection":
		newName := strings.Trim(strings.TrimSpace(getArg(args, 0)), `"'`)
		if err := RenameCollection(connID, database, coll, newName); err != nil {
			return RawResult{}, err
		}
		b, _ := json.Marshal(map[string]interface{}{"ok": 1})
		return RawResult{Docs: []json.RawMessage{json.RawMessage(b)}, Message: fmt.Sprintf("renamed to '%s'", newName)}, nil

	default:
		return RawResult{}, fmt.Errorf("unsupported operation '%s'. Supported: find, findOne, aggregate, insertOne, insertMany, updateOne, updateMany, deleteOne, deleteMany, countDocuments, estimatedDocumentCount, drop, renameCollection", op)
	}
}
