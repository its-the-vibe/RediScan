package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

var (
	redisClient *redis.Client
	ctx         = context.Background()
	maxLists    = 25 // Default max number of lists to display on index page
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
	http.HandleFunc("/key/", deleteKeyHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// ListInfo contains information about a Redis list
type ListInfo struct {
	Name string
	Size int64
}

// getAvailableLists retrieves a list of available Redis list keys with their sizes
func getAvailableLists() ([]ListInfo, error) {
	// Use SCAN instead of KEYS for better performance
	var lists []ListInfo
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
				// Skip this batch if pipeline fails, log and continue with next scan iteration
				log.Printf("Warning: Pipeline error, skipping batch: %v", err)
			} else {
				// First pass: identify which keys are lists
				var listKeys []string
				for i, key := range keys {
					keyType, err := typeCmds[i].Result()
					if err != nil {
						continue
					}
					if keyType == "list" {
						listKeys = append(listKeys, key)
					}
				}

				// Second pass: batch LLEN commands for confirmed lists only
				if len(listKeys) > 0 {
					sizePipeline := redisClient.Pipeline()
					llenCmds := make([]*redis.IntCmd, len(listKeys))
					for i, key := range listKeys {
						llenCmds[i] = sizePipeline.LLen(ctx, key)
					}
					_, err = sizePipeline.Exec(ctx)
					if err != nil {
						log.Printf("Warning: Pipeline error getting list sizes, skipping batch: %v", err)
					} else {
						for i, key := range listKeys {
							size, err := llenCmds[i].Result()
							if err != nil {
								continue
							}
							lists = append(lists, ListInfo{Name: key, Size: size})
							if len(lists) >= maxLists {
								return lists, nil
							}
						}
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

func sortLists(lists []ListInfo, sortBy string, order string) {
	sort.Slice(lists, func(i, j int) bool {
		if sortBy == "size" {
			if lists[i].Size != lists[j].Size {
				if order == "desc" {
					return lists[i].Size > lists[j].Size
				}
				return lists[i].Size < lists[j].Size
			}
			return lists[i].Name < lists[j].Name
		}
		if order == "desc" {
			return lists[i].Name > lists[j].Name
		}
		return lists[i].Name < lists[j].Name
	})
}

func deleteKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	keyPath := strings.TrimPrefix(r.URL.Path, "/key/")
	keyName, err := url.PathUnescape(keyPath)
	if err != nil || keyName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or missing key name"})
		return
	}

	deletedCount, err := redisClient.Del(ctx, keyName).Result()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to delete key: %v", err)})
		return
	}

	if deletedCount == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Key '%s' not found or already deleted", keyName)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Key '%s' successfully deleted", keyName)})
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	sortParam := strings.ToLower(r.URL.Query().Get("sort"))
	if sortParam != "size" {
		sortParam = "name"
	}

	orderParam := strings.ToLower(r.URL.Query().Get("order"))
	if orderParam != "desc" {
		orderParam = "asc"
	}

	// Get available Redis lists
	availableLists, err := getAvailableLists()
	if err != nil {
		log.Printf("Error fetching available lists: %v", err)
		// Continue even if we can't fetch lists
	} else {
		sortLists(availableLists, sortParam, orderParam)
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
        .sort-controls {
            display: flex;
            align-items: center;
            gap: 10px;
            margin-bottom: 15px;
            flex-wrap: wrap;
        }
        .sort-controls label {
            display: inline;
            margin-bottom: 0;
            font-weight: normal;
        }
        .sort-controls select {
            padding: 6px 10px;
            border: 1px solid #ddd;
            border-radius: 3px;
            font-size: 14px;
            background-color: white;
        }
        .list-item {
            padding: 10px;
            margin: 5px 0;
            background-color: #f9f9f9;
            border-radius: 3px;
            border-left: 3px solid #4CAF50;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .list-info-item {
            flex-grow: 1;
        }
        .list-item a {
            color: #2196F3;
            text-decoration: none;
            font-weight: 500;
        }
        .list-item a:hover {
            text-decoration: underline;
        }
        .list-size {
            color: #666;
            font-size: 14px;
        }
        .no-lists {
            color: #666;
            font-style: italic;
        }
        .delete-btn {
            background-color: #f44336;
            color: white;
            padding: 5px 10px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
            font-size: 13px;
            margin-left: 10px;
        }
        .delete-btn:hover {
            background-color: #d32f2f;
        }
        .alert {
            padding: 12px 20px;
            margin-bottom: 15px;
            border-radius: 4px;
            display: none;
        }
        .alert-success {
            background-color: #d4edda;
            color: #155724;
            border: 1px solid #c3e6cb;
        }
        .alert-error {
            background-color: #f8d7da;
            color: #721c24;
            border: 1px solid #f5c6cb;
        }
        .modal {
            display: none;
            position: fixed;
            z-index: 1000;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0,0,0,0.5);
            align-items: center;
            justify-content: center;
        }
        .modal-content {
            background-color: white;
            padding: 25px;
            border-radius: 5px;
            max-width: 450px;
            width: 90%;
            box-shadow: 0 4px 8px rgba(0,0,0,0.2);
        }
        .modal-content h3 {
            margin-top: 0;
            color: #333;
        }
        .modal-actions {
            display: flex;
            justify-content: flex-end;
            gap: 10px;
            margin-top: 20px;
        }
        .cancel-btn {
            background-color: #9e9e9e;
            color: white;
            padding: 8px 16px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
        }
        .cancel-btn:hover {
            background-color: #757575;
        }
        .confirm-delete-btn {
            background-color: #f44336;
            color: white;
            padding: 8px 16px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
        }
        .confirm-delete-btn:hover {
            background-color: #d32f2f;
        }
    </style>
</head>
<body>
    <h1>RediScan - Redis List Inspector</h1>
    <div class="info">
        <p>This tool allows you to inspect Redis lists with automatic JSON pretty-printing.</p>
        <p>Use cursor keys to navigate through list elements once loaded.</p>
    </div>

    <div id="alertBanner" class="alert"></div>

    <div class="available-lists">
        <h2>Available Redis Lists</h2>
        <form id="sortForm" method="get" action="/" class="sort-controls">
            <label for="sortSelect">Sort by:</label>
            <select id="sortSelect" name="sort" onchange="document.getElementById('sortForm').submit()">
                <option value="name" {{if eq .Sort "name"}}selected{{end}}>Name</option>
                <option value="size" {{if eq .Sort "size"}}selected{{end}}>Number of values</option>
            </select>

            <label for="orderSelect">Order:</label>
            <select id="orderSelect" name="order" onchange="document.getElementById('sortForm').submit()">
                <option value="asc" {{if eq .Order "asc"}}selected{{end}}>Ascending</option>
                <option value="desc" {{if eq .Order "desc"}}selected{{end}}>Descending</option>
            </select>
        </form>

        {{if .AvailableLists}}
        {{range .AvailableLists}}
        <div class="list-item" id="item-{{.Name}}">
            <div class="list-info-item">
                <a href="/lindex?key={{.Name | urlquery}}">{{.Name}}</a> <span class="list-size">({{.Size}} element{{if ne .Size 1}}s{{end}})</span>
            </div>
            <button class="delete-btn" onclick="openDeleteModal({{.Name}})">Delete</button>
        </div>
        {{end}}
        {{else}}
        <p class="no-lists">No Redis lists found. Create a list in Redis to get started.</p>
        {{end}}
    </div>

    <!-- Delete Confirmation Modal -->
    <div id="deleteModal" class="modal">
        <div class="modal-content">
            <h3>Confirm Key Deletion</h3>
            <p>Are you sure you want to delete the key <strong id="deleteKeyName"></strong>?</p>
            <p style="color: #666; font-size: 14px;">This action cannot be undone.</p>
            <div class="modal-actions">
                <button class="cancel-btn" onclick="closeDeleteModal()">Cancel</button>
                <button id="confirmDeleteBtn" class="confirm-delete-btn" onclick="confirmDelete()">Delete Key</button>
            </div>
        </div>
    </div>

    <script>
        let keyToDelete = '';

        function showAlert(message, type) {
            const banner = document.getElementById('alertBanner');
            banner.textContent = message;
            banner.className = 'alert alert-' + type;
            banner.style.display = 'block';
            window.scrollTo({ top: 0, behavior: 'smooth' });
        }

        function openDeleteModal(key) {
            keyToDelete = key;
            document.getElementById('deleteKeyName').textContent = "'" + key + "'";
            document.getElementById('deleteModal').style.display = 'flex';
        }

        function closeDeleteModal() {
            keyToDelete = '';
            document.getElementById('deleteModal').style.display = 'none';
        }

        function confirmDelete() {
            if (!keyToDelete) return;
            const targetKey = keyToDelete;
            closeDeleteModal();

            fetch('/key/' + encodeURIComponent(targetKey), {
                method: 'DELETE'
            })
            .then(async response => {
                const data = await response.json();
                if (response.ok) {
                    showAlert(data.message || "Key successfully deleted.", 'success');
                    setTimeout(() => {
                        window.location.reload();
                    }, 1000);
                } else {
                    showAlert(data.error || "Failed to delete key.", 'error');
                }
            })
            .catch(err => {
                showAlert("Error deleting key: " + err.message, 'error');
            });
        }

        // Close modal when clicking outside modal content
        window.onclick = function(event) {
            const modal = document.getElementById('deleteModal');
            if (event.target === modal) {
                closeDeleteModal();
            }
        };
    </script>
    <form action="/lindex" method="get">
        <label for="key">Redis List Key:</label>
        <input type="text" id="key" name="key" required placeholder="e.g., mylist">
        
        <label for="index">Index (optional, defaults to newest):</label>
        <input type="number" id="index" name="index" value="" min="0" placeholder="Leave empty for newest">
        
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
		AvailableLists []ListInfo
		Sort           string
		Order          string
	}{
		AvailableLists: availableLists,
		Sort:           sortParam,
		Order:          orderParam,
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

	// Parse index, defaulting to tail (newest item) if not provided
	var index int64
	if indexStr == "" {
		// Default to tail (last index, newest item)
		index = llen - 1
	} else {
		index, err = strconv.ParseInt(indexStr, 10, 64)
		if err != nil {
			renderNotFound(w, "Invalid 'index' parameter")
			return
		}
	}

	// Check bounds
	if index < 0 || index >= llen {
		renderNotFound(w, fmt.Sprintf("Index %d out of bounds (list length: %d)", index, llen))
		return
	}

	// Get all elements from the list at once for instant navigation
	// Note: All Redis lists in this system are guaranteed to be small enough to preload
	allValues, err := redisClient.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		renderError(w, fmt.Sprintf("Error getting list elements: %v", err))
		return
	}

	// Pretty-print all JSON values
	prettyValues := make([]string, len(allValues))
	for i, value := range allValues {
		prettyValues[i] = prettyPrintJSON(value)
	}

	// Render the result with all values preloaded
	renderResultWithPreload(w, key, index, llen, prettyValues)
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

func renderResultWithPreload(w http.ResponseWriter, key string, index int64, llen int64, allValues []string) {
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
        h1 a {
            color: #333;
            text-decoration: none;
        }
        h1 a:hover {
            text-decoration: underline;
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
        .slider-container {
            background-color: white;
            padding: 15px;
            border-radius: 5px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .slider-container label {
            display: block;
            margin-bottom: 10px;
            font-weight: bold;
            text-align: center;
        }
        .slider-container input[type="range"] {
            width: 100%;
            height: 8px;
            border-radius: 5px;
            background: #d3d3d3;
            outline: none;
            -webkit-appearance: none;
        }
        .slider-container input[type="range"]::-webkit-slider-thumb {
            -webkit-appearance: none;
            appearance: none;
            width: 20px;
            height: 20px;
            border-radius: 50%;
            background: #2196F3;
            cursor: pointer;
        }
        .slider-container input[type="range"]::-moz-range-thumb {
            width: 20px;
            height: 20px;
            border-radius: 50%;
            background: #2196F3;
            cursor: pointer;
            border: none;
        }
        @media (max-width: 600px) {
            .slider-container input[type="range"]::-webkit-slider-thumb {
                width: 30px;
                height: 30px;
            }
            .slider-container input[type="range"]::-moz-range-thumb {
                width: 30px;
                height: 30px;
            }
        }
    </style>
</head>
<body>
    <h1><a href="/">RediScan - Redis List Inspector</a></h1>
    
    <div class="metadata">
        <p><strong>Key:</strong> {{.Key}}</p>
        <p><strong>Index:</strong> {{.Index}}</p>
        <p><strong>List Length:</strong> {{.LLen}}</p>
    </div>

    <div class="navigation">
        <button id="prevBtn" onclick="navigate(-1)">← Older (Left Arrow)</button>
        <div class="info">{{.Index}} / {{.MaxIndex}}</div>
        <button id="nextBtn" onclick="navigate(1)">Newer (Right Arrow) →</button>
    </div>

    <div class="slider-container">
        <label for="positionSlider">Navigate: <span id="sliderLabel">{{.Index}} / {{.MaxIndex}}</span></label>
        <input type="range" id="positionSlider" min="0" max="{{.MaxIndex}}" value="{{.Index}}" step="1">
    </div>

    <div class="value-container">
        <h2>Value:</h2>
        <pre id="valueDisplay">{{index .AllValues .Index}}</pre>
    </div>

    <a href="/" class="back-link">← Back to Home</a>

    <script>
        const key = {{.Key}};
        let currentIndex = {{.Index}};
        const maxIndex = {{.MaxIndex}};
        const allValues = {{.AllValuesJSON}};

        // Helper function to update the UI to show a specific index
        function updateToIndex(newIndex) {
            // Update the display with the preloaded value
            document.getElementById('valueDisplay').textContent = allValues[newIndex];
            
            // Update the metadata
            document.querySelector('.navigation .info').textContent = newIndex + ' / ' + maxIndex;
            
            // Update the slider
            document.getElementById('positionSlider').value = newIndex;
            document.getElementById('sliderLabel').textContent = newIndex + ' / ' + maxIndex;
            
            // Update the current index for next navigation
            currentIndex = newIndex;
        }

        function navigate(delta) {
            let newIndex = currentIndex + delta;
            // Check for wrap around
            if (newIndex < 0) {
                // Wrapping backwards (older than oldest): reload to get fresh data and show newest
                window.location.href = '/lindex?key=' + encodeURIComponent(key);
                return;
            } else if (newIndex > maxIndex) {
                // Wrapping forwards (newer than newest): wrap to oldest
                newIndex = 0;
            }
            
            updateToIndex(newIndex);
        }

        // Handle slider changes
        document.getElementById('positionSlider').addEventListener('input', function(event) {
            const newIndex = parseInt(event.target.value);
            updateToIndex(newIndex);
        });

        // Handle keyboard navigation
        document.addEventListener('keydown', function(event) {
            if (event.key === 'ArrowLeft' || event.key === 'Left') {
                event.preventDefault();
                navigate(-1);
            } else if (event.key === 'ArrowRight' || event.key === 'Right') {
                event.preventDefault();
                navigate(1);
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

	// Convert allValues to JSON for embedding in JavaScript
	allValuesJSON, err := json.Marshal(allValues)
	if err != nil {
		renderError(w, fmt.Sprintf("Error encoding values: %v", err))
		return
	}

	data := struct {
		Key           string
		Index         int64
		LLen          int64
		MaxIndex      int64
		AllValues     []string
		AllValuesJSON template.JS
	}{
		Key:           key,
		Index:         index,
		LLen:          llen,
		MaxIndex:      llen - 1,
		AllValues:     allValues,
		AllValuesJSON: template.JS(allValuesJSON),
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
