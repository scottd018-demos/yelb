package function

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-redis/redis"
)

const (
	// redis connection info
	redisHost     = "redis"
	redisPort     = 6379
	redisPassword = ""

	// postgres connection info
	databaseHost     = "postgres"
	databasePort     = 5432
	databaseName     = "yelb"
	databaseUsername = "postgres"
	databasePassword = ""

	// backend table/cache info
	pageViewsKey    = "pageviews"
	ihopKey         = "ihop"
	outbackKey      = "outback"
	chipotleKey     = "chipotle"
	buccaDiBeppoKey = "bucadibeppo"
)

type stats struct {
	Hostname  string `json:"hostname"`
	PageViews string `json:"pageviews"`
}

type vote struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Handle an HTTP Request.
func Handle(ctx context.Context, res http.ResponseWriter, req *http.Request) {
	// this application only supports a GET method.  return if we have anything but GET.
	if req.Method != http.MethodGet {
		res.WriteHeader(http.StatusBadRequest)
		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(res, `{"error": "'%s' is an invalid method.  only GET supported."}`, req.Method)

		return
	}

	// set common headers for the request
	req.Header.Add("Access-Control-Allow-Origin", "*")
	req.Header.Add("Access-Control-Allow-Headers", "Authorization,Accepts,Content-Type,X-CSRF-Token,X-Requested-With")
	req.Header.Add("Access-Control-Allow-Methods", "GET")

	// initialize the redis connection
	redisClient := initRedis()
	defer redisClient.Close()

	// initialize the postgres connection
	dbClient := initPostgres()
	defer dbClient.Close()

	// create a place to store our response
	var response string

	// select the correct method based on the path
	switch req.URL.Path {
	case "/api/pageviews":
		response = getPageViews(redisClient)
	case "/api/hostname":
		response = getHostname()
	case "/api/getstats":
		response = getStats(redisClient)
	case "/api/getvotes":
		response = getVotes(dbClient)
	case "/api/ihop":
		response = updateRestaurant(dbClient, ihopKey)
	case "/api/chipotle":
		response = updateRestaurant(dbClient, chipotleKey)
	case "/api/outback":
		response = updateRestaurant(dbClient, outbackKey)
	case "/api/bucadibeppo":
		response = updateRestaurant(dbClient, buccaDiBeppoKey)
	default:
		res.WriteHeader(http.StatusBadRequest)
		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(res, `{"error": "'%s' is an invalid path"}`, req.URL.Path)

		return
	}

	// write and return the response
	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprint(res, response)
}

func getPageViews(redisClient *redis.Client) string {
	redisClient.Incr(pageViewsKey)
	count, err := redisClient.Get(pageViewsKey).Result()
	if err != nil {
		fmt.Printf("error: unable to get pageviews - %s", err)

		return "0"
	}

	return count
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("error: unable to get hostname - %s", err)

		return ""
	}

	return hostname
}

func getStats(redisClient *redis.Client) string {
	current := &stats{
		Hostname:  getHostname(),
		PageViews: getPageViews(redisClient),
	}

	jsonStats, err := json.Marshal(current)
	if err != nil {
		fmt.Printf("error: unable to get stats - %s", err)

		return "{}"
	}

	return string(jsonStats)
}

func getVotes(dbClient *sql.DB) string {
	var votes []vote

	for _, restaurant := range []string{ihopKey, chipotleKey, outbackKey, buccaDiBeppoKey} {
		vote := vote{Name: restaurant, Value: readCountPostgres(dbClient, restaurant)}

		votes = append(votes, vote)
	}

	jsonVotes, err := json.Marshal(votes)
	if err != nil {
		fmt.Printf("error: unable to get votes - %s", err)

		return "{}"
	}

	return string(jsonVotes)
}

func updateRestaurant(dbClient *sql.DB, restaurant string) string {
	return updateCountPostgres(dbClient, restaurant)
}

// helpers
func envStringOrDefault(env string, defaultValue string) string {
	fromEnv := os.Getenv(env)
	if fromEnv != "" {
		return fromEnv
	}

	return defaultValue
}

func envIntOrDefault(env string, defaultValue int) int {
	fromEnv := os.Getenv(env)
	if fromEnv != "" {
		envInt, err := strconv.Atoi(fromEnv)
		if err != nil {
			panic(fmt.Sprintf("value %s from environment variable %s is not an integer", fromEnv, env))
		}

		return envInt
	}

	return defaultValue
}

func initRedis() *redis.Client {
	// set the redis options
	options := &redis.Options{
		Addr: fmt.Sprintf("%s:%d", envStringOrDefault("REDIS_SERVER_ENDPOINT", redisHost), envIntOrDefault("REDIS_SERVER_PORT", redisPort)),
		DB:   0,
	}

	password := envStringOrDefault("REDIS_SERVER_ENDPOINT", redisHost)
	if password != "" {
		options.Password = password
	}

	client := redis.NewClient(options)

	// ping the Redis server to test the connection
	_, err := client.Ping().Result()
	if err != nil {
		client.Close()
		panic(fmt.Sprintf("error connecting to redis: %s", err))
	}

	return client
}

func initPostgres() *sql.DB {
	// create the connection string
	connStr := fmt.Sprintf(
		"host=%s port=%d dbname=%s sslmode=disable",
		envStringOrDefault("YELB_DB_SERVER_ENDPOINT", databaseHost),
		envIntOrDefault("YELB_DB_SERVER_PORT", databasePort),
		envStringOrDefault("YELB_DB_NAME", databaseName),
	)

	password := envStringOrDefault("YELB_DB_PASSWORD", databasePassword)
	if password != "" {
		connStr += fmt.Sprintf(
			" user=%s password=%s",
			envStringOrDefault("YELB_DB_USERNAME", databaseUsername),
			password,
		)
	}

	// open a database connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		db.Close()
		panic(fmt.Sprintf("error connecting to postgres: %s", err))
	}

	// ping the database to test the connection
	err = db.Ping()
	if err != nil {
		db.Close()
		panic(fmt.Sprintf("error pinging postgres: %s", err))
	}

	return db
}

func readCountPostgres(dbClient *sql.DB, restaurant string) string {
	// prepare the statement
	statement, err := dbClient.Prepare("SELECT count FROM restaurants WHERE name = ?")
	if err != nil {
		fmt.Printf("error: unable to prepare statement for restaurant read %s - %s", restaurant, err)

		return "0"
	}
	defer statement.Close()

	// execute the query
	var count string

	err = statement.QueryRow(restaurant).Scan(&count)
	if err != nil {
		fmt.Printf("error: unable execute statement for restaurant read %s - %s", restaurant, err)

		return "0"
	}

	return count
}

func updateCountPostgres(dbClient *sql.DB, restaurant string) string {
	// prepare the statement
	statement, err := dbClient.Prepare("UPDATE restaurants SET count = count +1 WHERE name = ?")
	if err != nil {
		fmt.Printf("error: unable to prepare statement for restaurant update %s - %s", restaurant, err)

		return "0"
	}
	defer statement.Close()

	// execute the update
	_, err = statement.Exec(restaurant)
	if err != nil {
		fmt.Printf("error: unable execute statement for restaurant update %s - %s", restaurant, err)

		return "0"
	}

	// return the latest value from the table
	return readCountPostgres(dbClient, restaurant)
}
