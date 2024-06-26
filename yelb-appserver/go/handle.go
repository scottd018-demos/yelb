package function

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/go-redis/redis"
	_ "github.com/lib/pq"
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
	pageViewAttr    = "counter" // dynamodb only
	ihopKey         = "ihop"
	outbackKey      = "outback"
	chipotleKey     = "chipotle"
	buccaDiBeppoKey = "bucadibeppo"
)

type stats struct {
	Hostname  string `json:"hostname"`
	PageViews int    `json:"pageviews"`
}

type vote struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type cacheClient struct {
	ctx           context.Context
	redis         *redis.ClusterClient
	dynamo        *dynamodb.Client
	dynamoDBTable string
}

func (cache *cacheClient) get() string {
	if cache.redis != nil {
		count, err := cache.redis.Get(pageViewsKey).Result()
		if err != nil {
			fmt.Printf("error: unable to get pageviews - %s", err)

			return "0"
		}

		return count
	}

	// use dynamodb connection
	output, err := cache.dynamo.GetItem(cache.ctx, &dynamodb.GetItemInput{
		TableName: aws.String(cache.dynamoDBTable),
		Key: map[string]types.AttributeValue{
			pageViewAttr: &types.AttributeValueMemberS{
				Value: pageViewsKey,
			},
		},
	})
	if err != nil {
		fmt.Printf("error: unable to get pageviews - %s", err)

		return "0"
	}

	item := output.Item[fmt.Sprintf("%scount", pageViewsKey)]
	switch v := item.(type) {
	case *types.AttributeValueMemberS:
		return v.Value
	default:
		panic(fmt.Sprintf("incorrect value type return %T for key %s", v, pageViewsKey))
	}
}

func (cache *cacheClient) close() {
	if cache.dynamo != nil {
		return
	}

	cache.redis.Close()
}

func (cache *cacheClient) increment() int {
	if cache.redis != nil {
		cache.redis.Incr(pageViewsKey)
		count, err := cache.redis.Get(pageViewsKey).Result()
		if err != nil {
			fmt.Printf("error: unable to get pageviews - %s", err)

			return 0
		}

		countInt, err := strconv.Atoi(count)
		if err != nil {
			panic(fmt.Sprintf("error: unable to convert pageviews to integer - %s", err))
		}

		return countInt
	}

	// use dynamodb connection
	count := cache.get()
	countInt, err := strconv.Atoi(count)
	if err != nil {
		panic(fmt.Sprintf("error: unable to convert pageviews to integer - %s", err))
	}

	updateItemInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(cache.dynamoDBTable),
		Key: map[string]types.AttributeValue{
			pageViewAttr: &types.AttributeValueMemberS{
				Value: pageViewsKey,
			},
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":c": &types.AttributeValueMemberS{
				Value: strconv.Itoa(countInt),
			},
		},
		UpdateExpression: aws.String(fmt.Sprintf("set %scount = :c", pageViewsKey)),
		ReturnValues:     types.ReturnValueUpdatedNew,
	}

	// run update
	_, err = cache.dynamo.UpdateItem(cache.ctx, updateItemInput)
	if err != nil {
		panic(fmt.Sprintf("unable to update dynamodb key %s", pageViewsKey))
	}

	return countInt
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

	// set common headers for the response
	res.Header().Add("Access-Control-Allow-Origin", "*")
	res.Header().Add("Access-Control-Allow-Headers", "Authorization,Accepts,Content-Type,X-CSRF-Token,X-Requested-With")
	res.Header().Add("Access-Control-Allow-Methods", "GET")
	res.Header().Add("Content-Type", "application/json")

	// initialize the cache connection
	cache := initCacheClient(ctx)
	defer cache.close()

	// initialize the postgres connection
	dbClient := initPostgres()
	defer dbClient.Close()

	// create a place to store our response
	var response string

	// retrieve the query parameters
	// NOTE: the following takes in api_path as a query parameter, e.g. /?api_path=/api/hostname.  This is because
	//       knative func currently only accepts traffic to the / endpoint, so we are faking this a bit.
	apiPath := normalizeApiPath(req.URL.Query().Get("api_path"))

	// select the correct method based on the path
	// NOTE: we are faking the api path here because knative func does not support pathing in the URL.  the
	//       handler only handles a '/'.
	switch apiPath {
	case "/api/pageviews":
		response = fmt.Sprint(getPageViews(cache))
	case "/api/hostname":
		response = getHostname()
	case "/api/getstats":
		response = getStats(cache)
	case "/api/getvotes":
		response = getVotes(dbClient)
	case "/api/ihop":
		response = fmt.Sprint(updateRestaurant(dbClient, ihopKey))
	case "/api/chipotle":
		response = fmt.Sprint(updateRestaurant(dbClient, chipotleKey))
	case "/api/outback":
		response = fmt.Sprint(updateRestaurant(dbClient, outbackKey))
	case "/api/bucadibeppo":
		response = fmt.Sprint(updateRestaurant(dbClient, buccaDiBeppoKey))
	default:
		res.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(res, `{"error": "'%s' is an invalid api_path"}`, apiPath)

		return
	}

	// write and return the response
	res.WriteHeader(http.StatusOK)
	fmt.Fprint(res, response)
}

func getPageViews(cache *cacheClient) int {
	return cache.increment()
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("error: unable to get hostname - %s", err)

		return ""
	}

	return hostname
}

func getStats(cache *cacheClient) string {
	current := &stats{
		Hostname:  getHostname(),
		PageViews: getPageViews(cache),
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

func updateRestaurant(dbClient *sql.DB, restaurant string) int {
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

func initRedis() *redis.ClusterClient {
	// set the redis options
	options := &redis.ClusterOptions{
		Addrs: []string{
			fmt.Sprintf("%s:%d", envStringOrDefault("REDIS_SERVER_ENDPOINT", redisHost), envIntOrDefault("REDIS_SERVER_PORT", redisPort)),
		},
	}

	hasTls := envStringOrDefault("REDIS_TLS", "")
	if hasTls == "true" {
		options.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	password := envStringOrDefault("REDIS_PASSWORD", "")
	if password != "" {
		options.Password = password
	}

	redisClient := redis.NewClusterClient(options)

	// ping the Redis server to test the connection
	_, err := redisClient.Ping().Result()
	if err != nil {
		redisClient.Close()
		panic(fmt.Sprintf("error connecting to redis: %s", err))
	}

	return redisClient
}

func initDynamoDB(ctx context.Context) *dynamodb.Client {
	// load aws config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(fmt.Sprintf("error loading AWS SDK config - %s", err.Error()))
	}

	// create dynamodb client and return
	return dynamodb.NewFromConfig(cfg)
}

func initCacheClient(ctx context.Context) *cacheClient {
	if envStringOrDefault("DYNAMODB_SERVER_TABLE", "") != "" {
		return &cacheClient{
			ctx:           ctx,
			dynamo:        initDynamoDB(ctx),
			dynamoDBTable: envStringOrDefault("DYNAMODB_SERVER_TABLE", ""),
		}
	}

	return &cacheClient{redis: initRedis()}
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
	dbClient, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(fmt.Sprintf("error connecting to postgres: %s", err))
	}

	// ping the database to test the connection
	err = dbClient.Ping()
	if err != nil {
		dbClient.Close()
		panic(fmt.Sprintf("error pinging postgres: %s", err))
	}

	return dbClient
}

func readCountPostgres(dbClient *sql.DB, restaurant string) int {
	// prepare the statement
	statement, err := dbClient.Prepare("SELECT count FROM restaurants WHERE name = $1")
	if err != nil {
		fmt.Printf("error: unable to prepare statement for restaurant read %s - %s\n", restaurant, err)

		return 0
	}
	defer statement.Close()

	// execute the query
	var count int

	err = statement.QueryRow(restaurant).Scan(&count)
	if err != nil {
		fmt.Printf("error: unable execute statement for restaurant read %s - %s\n", restaurant, err)

		return 0
	}

	return count
}

func updateCountPostgres(dbClient *sql.DB, restaurant string) int {
	// prepare the statement
	statement, err := dbClient.Prepare("UPDATE restaurants SET count = count +1 WHERE name = $1")
	if err != nil {
		fmt.Printf("error: unable to prepare statement for restaurant update %s - %s\n", restaurant, err)

		return 0
	}
	defer statement.Close()

	// execute the update
	_, err = statement.Exec(restaurant)
	if err != nil {
		fmt.Printf("error: unable execute statement for restaurant update %s - %s\n", restaurant, err)

		return 0
	}

	// return the latest value from the table
	return readCountPostgres(dbClient, restaurant)
}

func normalizeApiPath(apiPath string) string {
	// convert / characters to strings and store as an array
	pathArray := strings.Split(apiPath, "/")

	// remove empty spaces
	finalPathArray := []string{}
	for _, element := range pathArray {
		if element != "" {
			finalPathArray = append(finalPathArray, element)
		}
	}

	return fmt.Sprintf("/%s", strings.Join(finalPathArray, "/"))
}
