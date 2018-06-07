package main

import (
	elastic "gopkg.in/olivere/elastic.v3"
	"fmt"
	"net/http"
	"encoding/json"
	"log"
	"strconv"
	"reflect"
	"github.com/pborman/uuid"
	"strings"
	"context"
	"cloud.google.com/go/storage"
	"io"
	"cloud.google.com/go/bigtable"
	"github.com/auth0/go-jwt-middleware"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	
	
)

var mySigningKey = []byte("secret")

type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Post struct {
	// `json:"user"` is for the json parsing of this User field. Otherwise, by default it's 'User'.
	User     string `json:"user"`
	Message  string  `json:"message"`
	Location Location `json:"location"`
	Url string `json:"url"`
}


const (
	INDEX = "around"
	TYPE = "post"
	DISTANCE = "200km"
	// Needs to update
	PROJECT_ID = "ultra-might-203710"
	BT_INSTANCE = "circle-post"
	// Needs to update this URL if you deploy it to cloud.
	ES_URL = "http://35.237.6.51:9200"
	BUCKET_NAME = "post-images-ultra-might-203710"
)


func main() {
	// Create a client
	client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		panic(err)
		return
	}

	// Use the IndexExists service to check if a specified index exists.
	exists, err := client.IndexExists(INDEX).Do()
	if err != nil {
		panic(err)
	}
	if !exists {
		// Create a new index.
		mapping := `{
                    "mappings":{
                           "post":{
                                  "properties":{
                                         "location":{
                                                "type":"geo_point"
                                         }
                                  }
                           }
                    }
             }
             `
		_, err := client.CreateIndex(INDEX).Body(mapping).Do()
		if err != nil {
			// Handle error
			panic(err)
		}
	}

	fmt.Println("started-service")
	r := mux.NewRouter()
	var jwtMiddleware = jwtmiddleware.New(jwtmiddleware.Options{
		ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
			return mySigningKey, nil
		},
		SigningMethod: jwt.SigningMethodHS256,
	})



	r.Handle("/post", jwtMiddleware.Handler(http.HandlerFunc(handlerPost))).Methods("POST")
	r.Handle("/search", jwtMiddleware.Handler(http.HandlerFunc(PreflightHandle))).Methods("OPTIONS")
	r.Handle("/search", jwtMiddleware.Handler(http.HandlerFunc(handlerSearch))).Methods("GET")
	r.Handle("/login", http.HandlerFunc(loginHandler)).Methods("POST")
	r.Handle("/signup", http.HandlerFunc(signupHandler)).Methods("POST")

	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(":8080", nil))


}

func PreflightHandle(w http.ResponseWriter, r *http.Request) {
	fmt.Println("received an OPTIONS request")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
    	w.WriteHeader(http.StatusOK)
    	return
	}
}

func handlerSearch (w http.ResponseWriter, r *http.Request) {
	
	fmt.Println("received a search request")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
	//w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")



	if r.Method != "GET" {
		return
	}

	lat := r.URL.Query().Get("lat")
	lt , _ := strconv.ParseFloat(lat, 64)
	lon := r.URL.Query().Get("lon")
	ln , _ := strconv.ParseFloat(lon, 64)

	//optional parameter
	ran := DISTANCE
	if val := r.URL.Query().Get("range"); val != "" {
		ran = val + "km"
	}
	fmt.Printf( "Search received: %f %f %s\n", lat, lon, ran)

	// Create a client
	client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		panic(err)
		return
	}

	// Define geo distance query as specified in
	// https://www.elastic.co/guide/en/elasticsearch/reference/5.2/query-dsl-geo-distance-query.html
	q := elastic.NewGeoDistanceQuery("location")
	q = q.Distance(ran).Lat(lt).Lon(ln)

	// Some delay may range from seconds to minutes. So if you don't get enough results. Try it later.
	searchResult, err := client.Search().
		Index(INDEX).
		Query(q).
		Pretty(true).
		Do()
	if err != nil {
		// Handle error
		panic(err)
	}

	// searchResult is of type SearchResult and returns hits, suggestions,
	// and all kinds of other information from Elasticsearch.
	fmt.Printf("Query took %d milliseconds\n", searchResult.TookInMillis)
	// TotalHits is another convenience function that works even when something goes wrong.
	fmt.Printf("Found a total of %d post\n", searchResult.TotalHits())

	// Each is a convenience function that iterates over hits in a search result.
	// It makes sure you don't need to check for nil values in the response.
	// However, it ignores errors in serialization.
	var typ Post
	var ps []Post
	for _, item := range searchResult.Each(reflect.TypeOf(typ)) { // instance of
		p := item.(Post) // p = (Post) item
		fmt.Printf("Post by %s: %s at lat %v and lon %v\n", p.User, p.Message, p.Location.Lat, p.Location.Lon)
		// TODO(student homework): Perform filtering based on keywords such as web spam etc.
		if !containsFilteredWords(&p.Message) {
			ps = append(ps,p)
		}

	}
	js, err := json.Marshal(ps)
	if err != nil {
		panic(err)
		return
	}

	w.Write(js)
}



func handlerPost(w http.ResponseWriter, r *http.Request) {
	// Parse from body of request to get a json object.
	//fmt.Println("Received one post request")
	//decoder := json.NewDecoder(r.Body)
	//var p Post
	//if err := decoder.Decode(&p); err != nil {
	//	panic(err)
	//	return
	//}
	//id := uuid.New()
	//// Save to ES.
	//saveToES(&p, id)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")

	if r.Method != "POST" {
		return
	}

	user := r.Context().Value("user")
	if user == nil {
		m := fmt.Sprintf("Unable to find user in context")
		fmt.Println(m)
		http.Error(w, m, http.StatusBadRequest)
		return
	}
	claims := user.(*jwt.Token).Claims
	username := claims.(jwt.MapClaims)["username"]



	// 32 << 20 is the maxMemory param for ParseMultipartForm, equals to 32MB (1MB = 1024 * 1024 bytes = 2^20 bytes)
	// After you call ParseMultipartForm, the file will be saved in the server memory with maxMemory size.
	// If the file size is larger than maxMemory, the rest of the data will be saved in a system temporary file.
	r.ParseMultipartForm(32 << 20)

	// Parse from form data.
	fmt.Printf("Received one post request %s\n", r.FormValue("message"))
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lon, _ := strconv.ParseFloat(r.FormValue("lon"), 64)
	p := &Post{
		User:    username.(string),
		Message: r.FormValue("message"),
		Location: Location{
			Lat: lat,
			Lon: lon,
		},
	}

	id := uuid.New()

	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Image is not available", http.StatusInternalServerError)
		fmt.Printf("Image is not available %v.\n", err)
		return
	}

	ctx := context.Background()
	defer file.Close()
	//GCS
	// replace it with your real bucket name.
	_, attrs, err := saveToGCS(ctx, file, BUCKET_NAME, id)
	if err != nil {
		http.Error(w, "GCS is not setup", http.StatusInternalServerError)
		fmt.Printf("GCS is not setup %v\n", err)
		return
	}

	// Update the media link after saving to GCS.
	p.Url = attrs.MediaLink

	// Save to ES.
	saveToES(p, id)

	// Save to BigTable.
	//saveToBigTable(p, id)


}

// Save a post to ElasticSearch
func saveToES(p *Post, id string) {
	// Create a client
	es_client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		panic(err)
		return
	}

	// Save it to index
	_, err = es_client.Index().
		Index(INDEX).
		Type(TYPE).
		Id(id).
		BodyJson(p).
		Refresh(true).
		Do()
	if err != nil {
		panic(err)
		return
	}

	fmt.Printf("Post is saved to Index: %s\n", p.Message)
}

func containsFilteredWords(s * string) bool {
	filteredWords := []string {"shit", "bitch"}
	for _, word := range filteredWords {
		if strings.Contains(*s ,word) {
			return true
		}
	}
	return false
}

func saveToGCS(ctx context.Context, r io.Reader, bucketName, name string) (*storage.ObjectHandle, *storage.ObjectAttrs, error) {
	// Student questions
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	bucket := client.Bucket(bucketName)
	// check if the bucket exists
	if _, err = bucket.Attrs(ctx); err != nil {
		return nil, nil, err
	}
	//OBJ == FILE
	obj := bucket.Object(name)
	w := obj.NewWriter(ctx)
	if _ , err := io.Copy(w, r); err != nil {
		return nil, nil, err
	}

	if err := w.Close(); err != nil {
		return nil, nil, err
	}

	//every body can read
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return nil, nil, err
	}

	//get file URL
	attrs, err := obj.Attrs(ctx)
	fmt.Printf("Post is saved to GCS: %s\n", attrs.MediaLink)

	return obj, attrs, err
}
// Save a post to BigTable
func saveToBigTable(p *Post, id string) {
	ctx := context.Background()
	bt_client, err := bigtable.NewClient(ctx, PROJECT_ID, BT_INSTANCE)
	if err != nil {
		panic(err)
		return
	}

	tbl := bt_client.Open("post")
	mut := bigtable.NewMutation()
	t := bigtable.Now()
	mut.Set("post", "user", t, []byte(p.User))
	mut.Set("post", "message", t, []byte(p.Message))
	mut.Set("location", "lat", t, []byte(strconv.FormatFloat(p.Location.Lat, 'f', -1, 64)))
	mut.Set("location", "lon", t, []byte(strconv.FormatFloat(p.Location.Lon, 'f', -1, 64)))
	mut.Set("post","Url", t, []byte(p.Url))

	err = tbl.Apply(ctx, id, mut)
	if err != nil {
		panic(err)
		return
	}
	fmt.Printf("Post is saved to BigTable: %s\n", p.Message)

	//bt_client, err := bigtable.NewClient(ctx, PROJECT_ID, BT_INSTANCE)
	//if err != nil {
	//	panic(err)
	//	return
	//}
	//
	//// TODO (student questions) save Post into BT as well
	//tbl := bt_client.Open("post")
	//mut := bigtable.NewMutation()
	//t := bigtable.Now()
	//
	//mut.Set("post", "user", t, []byte(p.User))
	//mut.Set("post", "message", t, []byte(p.Message))
	//mut.Set("location", "lat", t, []byte(strconv.FormatFloat(p.Location.Lat, 'f', -1, 64)))
	//mut.Set("location", "lon", t, []byte(strconv.FormatFloat(p.Location.Lon, 'f', -1, 64)))
	//mut.Set("post","Url", t, []byte(p.Url))
	//
	//err = tbl.Apply(ctx, id, mut)
	//if err != nil {
	//	panic(err)
	//	return
	//}
	//fmt.Printf("Post is saved to BigTable: %s\n", p.Message)
}
