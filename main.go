package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
)

var (
	redisClient *redis.Client
	ctx         = context.Background()
	maxLists    = 10 // Default max number of lists to display on index page
)

func main() {
	// Initialize Redis client
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDB := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if db, err := strconv.Atoi(dbStr); err == nil {
			redisDB = db
		}
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})

	// Configure max lists to display
	if maxListsStr := os.Getenv("MAX_LISTS"); maxListsStr != "" {
		if ml, err := strconv.Atoi(maxListsStr); err == nil && ml > 0 {
			maxLists = ml
		}
	}

	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Could not connect to Redis at %s: %v", redisAddr, err)
	} else {
		log.Printf("Connected to Redis at %s", redisAddr)
	}

	// Setup HTTP handlers
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/lindex", lindexHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// getAvailableLists retrieves a list of available Redis list keys
func getAvailableLists() ([]string, error) {
	// Use SCAN instead of KEYS for better performance
	var lists []string
	var cursor uint64
	
	for {
		var keys []string
		var err error
		keys, cursor, err = redisClient.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}

		// Use pipeline to batch TYPE commands for better performance
		if len(keys) > 0 {
			pipe := redisClient.Pipeline()
			typeCmds := make([]*redis.StatusCmd, len(keys))
			for i, key := range keys {
				typeCmds[i] = pipe.Type(ctx, key)
			}
			_, err = pipe.Exec(ctx)
			if err != nil {
				// Continue even if pipeline fails
				log.Printf("Warning: Pipeline error: %v", err)
			}

			// Check results and filter for lists
			for i, key := range keys {
				keyType, err := typeCmds[i].Result()
				if err != nil {
					continue
				}
				if keyType == "list" {
					lists = append(lists, key)
					if len(lists) >= maxLists {
						return lists, nil
					}
				}
			}
		}

		if cursor == 0 {
			break
		}
	}

	return lists, nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get available Redis lists
	availableLists, err := getAvailableLists()
	if err != nil {
		log.Printf("Error fetching available lists: %v", err)
		// Continue even if we can't fetch lists
	}

	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>RediScan - Redis List Inspector</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        h1 {
            color: #333;
        }
        .info {
            background-color: #e7f3ff;
            padding: 15px;
            border-radius: 5px;
            margin-bottom: 20px;
        }
        form {
            background-color: white;
            padding: 20px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        label {
            display: block;
            margin-bottom: 5px;
            font-weight: bold;
        }
        input {
            width: 100%;
            padding: 8px;
            margin-bottom: 15px;
            border: 1px solid #ddd;
            border-radius: 3px;
            box-sizing: border-box;
        }
        button {
            background-color: #4CAF50;
            color: white;
            padding: 10px 20px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
            font-size: 16px;
        }
        button:hover {
            background-color: #45a049;
        }
        .available-lists {
            background-color: white;
            padding: 20px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 20px;
        }
        .available-lists h2 {
            margin-top: 0;
            color: #333;
        }
        .list-item {
            padding: 10px;
            margin: 5px 0;
            background-color: #f9f9f9;
            border-radius: 3px;
            border-left: 3px solid #4CAF50;
        }
        .list-item a {
            color: #2196F3;
            text-decoration: none;
            font-weight: 500;
        }
        .list-item a:hover {
            text-decoration: underline;
        }
        .no-lists {
            color: #666;
            font-style: italic;
        }
    </style>
</head>
<body>
    <h1>RediScan - Redis List Inspector</h1>
    <div class="info">
        <p>This tool allows you to inspect Redis lists with automatic JSON pretty-printing.</p>
        <p>Use cursor keys to navigate through list elements once loaded.</p>
    </div>
    {{if .AvailableLists}}
    <div class="available-lists">
        <h2>Available Redis Lists</h2>
        {{range .AvailableLists}}
        <div class="list-item">
            <a href="/lindex?key={{. | urlquery}}&index=0">{{.}}</a>
        </div>
        {{end}}
    </div>
    {{end}}
    <form action="/lindex" method="get">
        <label for="key">Redis List Key:</label>
        <input type="text" id="key" name="key" required placeholder="e.g., mylist">
        
        <label for="index">Index:</label>
        <input type="number" id="index" name="index" value="0" min="0" required>
        
        <button type="submit">Inspect</button>
    </form>
</body>
</html>`

	tmplParsed, err := template.New("index").Parse(tmpl)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}

	data := struct {
		AvailableLists []string
	}{
		AvailableLists: availableLists,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplParsed.Execute(w, data); err != nil {
		log.Printf("Error rendering template: %v", err)
	}
}

func lindexHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	indexStr := r.URL.Query().Get("index")

	if key == "" {
		renderNotFound(w, "Missing 'key' parameter")
		return
	}

	index, err := strconv.ParseInt(indexStr, 10, 64)
	if err != nil {
		renderNotFound(w, "Invalid 'index' parameter")
		return
	}

	// Check if key exists and is a list
	keyType, err := redisClient.Type(ctx, key).Result()
	if err != nil {
		renderError(w, fmt.Sprintf("Error checking key: %v", err))
		return
	}

	if keyType == "none" {
		renderNotFound(w, fmt.Sprintf("Key '%s' does not exist", key))
		return
	}

	if keyType != "list" {
		renderNotFound(w, fmt.Sprintf("Key '%s' is not a list (type: %s)", key, keyType))
		return
	}

	// Get list length
	llen, err := redisClient.LLen(ctx, key).Result()
	if err != nil {
		renderError(w, fmt.Sprintf("Error getting list length: %v", err))
		return
	}

	if llen == 0 {
		renderNotFound(w, fmt.Sprintf("List '%s' is empty", key))
		return
	}

	// Check bounds
	if index < 0 || index >= llen {
		renderNotFound(w, fmt.Sprintf("Index %d out of bounds (list length: %d)", index, llen))
		return
	}

	// Get the element at index
	value, err := redisClient.LIndex(ctx, key, index).Result()
	if err != nil {
		renderError(w, fmt.Sprintf("Error getting element: %v", err))
		return
	}

	// Try to pretty-print as JSON
	prettyValue := prettyPrintJSON(value)

	// Render the result
	renderResult(w, key, index, llen, prettyValue)
}

func prettyPrintJSON(value string) string {
	var jsonData interface{}
	if err := json.Unmarshal([]byte(value), &jsonData); err != nil {
		// Not valid JSON, return as-is
		return value
	}

	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		// Fallback to original value
		return value
	}

	return string(prettyJSON)
}

func renderResult(w http.ResponseWriter, key string, index int64, llen int64, value string) {
	tmplStr := `<!DOCTYPE html>
<html>
<head>
    <title>RediScan - {{.Key}}[{{.Index}}]</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        h1 {
            color: #333;
        }
        .metadata {
            background-color: white;
            padding: 15px;
            border-radius: 5px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .metadata p {
            margin: 5px 0;
        }
        .navigation {
            background-color: white;
            padding: 15px;
            border-radius: 5px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            display: flex;
            gap: 10px;
            align-items: center;
        }
        .navigation button {
            background-color: #2196F3;
            color: white;
            padding: 10px 20px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
            font-size: 16px;
        }
        .navigation button:hover {
            background-color: #0b7dda;
        }
        .navigation button:disabled {
            background-color: #ccc;
            cursor: not-allowed;
        }
        .navigation .info {
            flex-grow: 1;
            text-align: center;
            font-weight: bold;
        }
        .value-container {
            background-color: white;
            padding: 20px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        pre {
            background-color: #f4f4f4;
            padding: 15px;
            border-radius: 3px;
            overflow-x: auto;
            border: 1px solid #ddd;
            white-space: pre-wrap;
            word-wrap: break-word;
        }
        .back-link {
            display: inline-block;
            margin-top: 20px;
            color: #2196F3;
            text-decoration: none;
        }
        .back-link:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <h1>RediScan - Redis List Inspector</h1>
    
    <div class="metadata">
        <p><strong>Key:</strong> {{.Key}}</p>
        <p><strong>Index:</strong> {{.Index}}</p>
        <p><strong>List Length:</strong> {{.LLen}}</p>
    </div>

    <div class="navigation">
        <button id="prevBtn" {{if eq .Index 0}}disabled{{end}} onclick="navigate(-1)">← Previous (Left Arrow)</button>
        <div class="info">{{.Index}} / {{.MaxIndex}}</div>
        <button id="nextBtn" {{if eq .Index .MaxIndex}}disabled{{end}} onclick="navigate(1)">Next (Right Arrow) →</button>
    </div>

    <div class="value-container">
        <h2>Value:</h2>
        <pre>{{.Value}}</pre>
    </div>

    <a href="/" class="back-link">← Back to Home</a>

    <script>
        const key = {{.Key}};
        const currentIndex = {{.Index}};
        const maxIndex = {{.MaxIndex}};

        function navigate(delta) {
            const newIndex = currentIndex + delta;
            if (newIndex >= 0 && newIndex <= maxIndex) {
                window.location.href = '/lindex?key=' + encodeURIComponent(key) + '&index=' + newIndex;
            }
        }

        // Handle keyboard navigation
        document.addEventListener('keydown', function(event) {
            if (event.key === 'ArrowLeft' || event.key === 'Left') {
                event.preventDefault();
                if (currentIndex > 0) {
                    navigate(-1);
                }
            } else if (event.key === 'ArrowRight' || event.key === 'Right') {
                event.preventDefault();
                if (currentIndex < maxIndex) {
                    navigate(1);
                }
            }
        });
    </script>
</body>
</html>`

	tmpl, err := template.New("result").Parse(tmplStr)
	if err != nil {
		renderError(w, fmt.Sprintf("Template error: %v", err))
		return
	}

	data := struct {
		Key      string
		Index    int64
		LLen     int64
		MaxIndex int64
		Value    string
	}{
		Key:      key,
		Index:    index,
		LLen:     llen,
		MaxIndex: llen - 1,
		Value:    value,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Error rendering template: %v", err)
	}
}

func renderNotFound(w http.ResponseWriter, message string) {
	tmplStr := `<!DOCTYPE html>
<html>
<head>
    <title>Not Found - RediScan</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .error-container {
            background-color: white;
            padding: 40px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            text-align: center;
        }
        h1 {
            color: #d32f2f;
            font-size: 48px;
            margin: 0 0 20px 0;
        }
        p {
            color: #666;
            font-size: 18px;
            margin: 20px 0;
        }
        .back-link {
            display: inline-block;
            margin-top: 20px;
            color: #2196F3;
            text-decoration: none;
            font-size: 16px;
        }
        .back-link:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="error-container">
        <h1>404</h1>
        <p>{{.Message}}</p>
        <a href="/" class="back-link">← Back to Home</a>
    </div>
</body>
</html>`

	tmpl, err := template.New("notfound").Parse(tmplStr)
	if err != nil {
		http.Error(w, message, http.StatusNotFound)
		return
	}

	data := struct {
		Message string
	}{
		Message: message,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Error rendering template: %v", err)
	}
}

func renderError(w http.ResponseWriter, message string) {
	tmplStr := `<!DOCTYPE html>
<html>
<head>
    <title>Error - RediScan</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .error-container {
            background-color: white;
            padding: 40px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            text-align: center;
        }
        h1 {
            color: #d32f2f;
            font-size: 36px;
            margin: 0 0 20px 0;
        }
        p {
            color: #666;
            font-size: 18px;
            margin: 20px 0;
        }
        .back-link {
            display: inline-block;
            margin-top: 20px;
            color: #2196F3;
            text-decoration: none;
            font-size: 16px;
        }
        .back-link:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="error-container">
        <h1>Error</h1>
        <p>{{.Message}}</p>
        <a href="/" class="back-link">← Back to Home</a>
    </div>
</body>
</html>`

	tmpl, err := template.New("error").Parse(tmplStr)
	if err != nil {
		http.Error(w, message, http.StatusInternalServerError)
		return
	}

	data := struct {
		Message string
	}{
		Message: message,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Error rendering template: %v", err)
	}
}
