package pinecone

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pinecone-io/go-pinecone/internal/gen/control"
	"github.com/pinecone-io/go-pinecone/internal/provider"
	"github.com/pinecone-io/go-pinecone/internal/useragent"
	"google.golang.org/grpc"
)

// Client holds the parameters for connecting to the Pinecone service. It is returned by the NewClient and NewClientBase
// functions. To use Client, first build the parameters of the request using NewClientParams (or NewClientBaseParams).
// Then, pass those parameters into the NewClient (or NewClientBase) function to create a new Client object.
// Once instantiated, you can use Client to execute control plane API requests (e.g. create an Index, list Indexes,
// etc.). Read more about different control plane API routes at [docs.pinecone.io/reference/api].
//
// Note: Client methods are safe for concurrent use.
//
// Fields:
//   - headers: An optional map of additional HTTP headers to include in each API request to the control plane,
//     provided through NewClientParams.Headers or NewClientBaseParams.Headers.
//   - restClient: Optional underlying *http.Client object used to communicate with the Pinecone control plane API,
//     provided through NewClientParams.RestClient or NewClientBaseParams.RestClient. If not provided,
//     a default client is created for you.
//   - sourceTag: An optional string used to help Pinecone attribute API activity, provided through NewClientParams.SourceTag
//     or NewClientBaseParams.SourceTag.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams) // --> This creates a new Client object.
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    }
//
//	    idx, err := pc.DescribeIndex(ctx, "your-index-name")
//	    if err != nil {
//		       log.Fatalf("Failed to describe index \"%s\". Error:%s", idx.Name, err)
//	    } else {
//		       fmt.Printf("Successfully found the \"%s\" index!\n", idx.Name)
//	    }
//
//	    idxConnection, err := pc.Index(idx.Host)
//	    if err != nil {
//		       log.Fatalf("Failed to create IndexConnection for Host: %v. Error: %v", idx.Host, err)
//	    } else {
//		       log.Println("IndexConnection created successfully!")
//	    }
//
// [docs.pinecone.io/reference/api]: https://docs.pinecone.io/reference/api/control-plane/list_indexes
type Client struct {
	headers    map[string]string
	restClient *control.Client
	sourceTag  string
}

// NewClientParams holds the parameters for creating a new Client instance while authenticating via an API key.
//
// Fields:
//   - ApiKey: (Required) The API key used to authenticate with the Pinecone control plane API.
//     This value must be passed by the user unless it is set as an environment variable ("PINECONE_API_KEY").
//   - Headers: An optional map of additional HTTP headers to include in each API request to the control plane.
//   - Host: The host URL of the Pinecone control plane API. If not provided,
//     the default value is "https://api.pinecone.io".
//   - RestClient: An optional HTTP client to use for communication with the control plane API.
//   - SourceTag: An optional string used to help Pinecone attribute API activity.
//
// See Client for code example.
type NewClientParams struct {
	ApiKey     string            // required - provide through NewClientParams or environment variable PINECONE_API_KEY
	Headers    map[string]string // optional
	Host       string            // optional
	RestClient *http.Client      // optional
	SourceTag  string            // optional
}

// NewClientBaseParams holds the parameters for creating a new Client instance while passing custom authentication
// headers.
//
// Fields:
//   - Headers: An optional map of additional HTTP headers to include in each API request to the control plane.
//     "Authorization" and "X-Project-Id" headers are required if authenticating using a JWT.
//   - Host: The host URL of the Pinecone control plane API. If not provided,
//     the default value is "https://api.pinecone.io".
//   - RestClient: An optional *http.Client object to use for communication with the control plane API.
//   - SourceTag: An optional string used to help Pinecone attribute API activity.
//
// See Client for code example.
type NewClientBaseParams struct {
	Headers    map[string]string
	Host       string
	RestClient *http.Client
	SourceTag  string
}

// NewIndexConnParams holds the parameters for creating an IndexConnection to a Pinecone index.
//
// Fields:
//   - Host: The host URL of the Pinecone index. This is required. To find your host url use the DescribeIndex or ListIndexes methods.
//     Alternatively, the host is displayed in the Pinecone web console.
//   - Namespace: Optional index namespace to use for operations. If not provided, the default namespace of "" will be used.
//   - AdditionalMetdata: Optional additional metdata to be sent with each RPC request.
//
// See Client.Index for code example.
type NewIndexConnParams struct {
	Host               string            // required - obtained through DescribeIndex or ListIndexes
	Namespace          string            // optional - if not provided the default namespace of "" will be used
	AdditionalMetadata map[string]string // optional
}

// NewClient creates and initializes a new instance of Client.
// This function sets up the control plane client with the necessary configuration for authentication and communication.
//
// Parameters:
//   - in: A NewClientParams object. See NewClientParams for more information.
//
// Note: It is important to handle the error returned by this function to ensure that the
// control plane client has been created successfully before attempting to make API calls.
//
// Returns a pointer to an initialized Client instance or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
func NewClient(in NewClientParams) (*Client, error) {
	osApiKey := os.Getenv("PINECONE_API_KEY")
	hasApiKey := (valueOrFallback(in.ApiKey, osApiKey) != "")

	if !hasApiKey {
		return nil, fmt.Errorf("no API key provided, please pass an API key for authorization through NewClientParams or set the PINECONE_API_KEY environment variable")
	}

	apiKeyHeader := struct{ Key, Value string }{"Api-Key", valueOrFallback(in.ApiKey, osApiKey)}

	clientHeaders := in.Headers
	if clientHeaders == nil {
		clientHeaders = make(map[string]string)
		clientHeaders[apiKeyHeader.Key] = apiKeyHeader.Value

	} else {
		clientHeaders[apiKeyHeader.Key] = apiKeyHeader.Value
	}

	return NewClientBase(NewClientBaseParams{Headers: clientHeaders, Host: in.Host, RestClient: in.RestClient, SourceTag: in.SourceTag})
}

// NewClientBase creates and initializes a new instance of Client with custom authentication headers.
//
// Parameters:
//   - in: A NewClientBaseParams object that includes the necessary configuration for the control plane client. See
//     NewClientBaseParams for more information.
//
// Notes:
//   - It is important to handle the error returned by this function to ensure that the
//     control plane client has been created successfully before attempting to make API calls.
//   - A Pinecone API key is not requried when using NewClientBase.
//
// Returns a pointer to an initialized Client instance or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientBaseParams{
//	        Headers: map[string]string{
//	            "Authorization": "Bearer " + "<your JWT token>"
//	            "X-Project-Id": "<Your Pinecone project ID>"
//	        },
//	        SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClientBase(clientParams)
//		       if err != nil {
//	            log.Fatalf("Failed to create Client: %v", err)
//	        } else {
//		           fmt.Println("Successfully created a new Client object!")
//	    }
func NewClientBase(in NewClientBaseParams) (*Client, error) {
	clientOptions := buildClientBaseOptions(in)
	var err error

	controlHostOverride := valueOrFallback(in.Host, os.Getenv("PINECONE_CONTROLLER_HOST"))
	if controlHostOverride != "" {
		controlHostOverride, err = ensureURLScheme(controlHostOverride)
		if err != nil {
			return nil, err
		}
	}

	client, err := control.NewClient(valueOrFallback(controlHostOverride, "https://api.pinecone.io"), clientOptions...)
	if err != nil {
		return nil, err
	}

	c := Client{restClient: client, sourceTag: in.SourceTag, headers: in.Headers}
	return &c, nil
}

// Index creates an IndexConnection to a specified host.
//
// Parameters:
//   - in: A NewIndexConnParams object that includes the necessary configuration to create an IndexConnection.
//     See NewIndexConnParams for more information.
//
// Note: It is important to handle the error returned by this method to ensure that the IndexConnection is created
// successfully before making data plane calls.
//
// Returns a pointer to an IndexConnection instance or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//		       log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    idx, err := pc.DescribeIndex(ctx, "your-index-name")
//	    if err != nil {
//		       log.Fatalf("Failed to describe index \"%s\". Error:%s", idx.Name, err)
//	    } else {
//		       fmt.Printf("Successfully found the \"%s\" index!\n", idx.Name)
//	    }
//
//	    indexConnParams := pinecone.NewIndexConnParams{
//		       Host: idx.Host,
//		       Namespace: "your-namespace",
//		       AdditionalMetadata: map[string]string{
//			       "your-metadata-key": "your-metadata-value",
//		       },
//	    }
//
//	    idxConnection, err := pc.Index(indexConnParams)
//	    if err != nil {
//		       log.Fatalf("Failed to create IndexConnection for Host: %v. Error: %v", idx.Host, err)
//	    } else {
//		       log.Println("IndexConnection created successfully!")
//	    }
func (c *Client) Index(in NewIndexConnParams, dialOpts ...grpc.DialOption) (*IndexConnection, error) {
	// extract authHeader from Client which is used to authenticate the IndexConnection
	// merge authHeader with additionalMetadata provided in NewIndexConnParams
	authHeader := c.extractAuthHeader()
	if in.AdditionalMetadata != nil {
		for key, value := range authHeader {
			in.AdditionalMetadata[key] = value
		}
	} else {
		in.AdditionalMetadata = authHeader
	}

	idx, err := newIndexConnection(newIndexParameters{
		host:               in.Host,
		namespace:          in.Namespace,
		sourceTag:          c.sourceTag,
		additionalMetadata: in.AdditionalMetadata,
	}, dialOpts...)
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// ListIndexes retrieves a list of all Indexes in a Pinecone [project].
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//
// Returns a slice of pointers to Index objects or an error.
//
// Example:
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    idxs, err := pc.ListIndexes(ctx)
//	    if err != nil {
//		       log.Fatalf("Failed to list indexes: %v", err)
//	    } else {
//		       fmt.Println("Your project has the following indexes:")
//		       for _, idx := range idxs {
//			       fmt.Printf("- \"%s\"\n", idx.Name)
//		       }
//	    }
//
// [project]: https://docs.pinecone.io/guides/projects/understanding-projects
func (c *Client) ListIndexes(ctx context.Context) ([]*Index, error) {
	res, err := c.restClient.ListIndexes(ctx)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleErrorResponseBody(res, "failed to list indexes: ")
	}

	var indexList control.IndexList
	err = json.NewDecoder(res.Body).Decode(&indexList)
	if err != nil {
		return nil, err
	}

	indexes := make([]*Index, len(*indexList.Indexes))
	for i, idx := range *indexList.Indexes {
		indexes[i] = toIndex(&idx)
	}

	return indexes, nil
}

// CreatePodIndexRequest holds the parameters for creating a new pods-based Index.
//
// Fields:
//   - Name: The name of the Index. Resource name must be 1-45 characters long,
//     start and end with an alphanumeric character,
//     and consist only of lower case alphanumeric characters or '-'.
//   - Dimension: The [dimensionality] of the vectors to be inserted in the Index.
//   - Metric: The distance metric to be used for [similarity] search. You can use
//     'euclidean', 'cosine', or 'dotproduct'.
//   - Environment: The [cloud environment] where the Index will be hosted.
//   - PodType: The [type of pod] to use for the Index. One of `s1`, `p1`, or `p2` appended with `.` and
//     one of `x1`, `x2`, `x4`, or `x8`.
//   - Shards: The number of shards to use for the Index (defaults to 1).
//     Shards split your data across multiple pods, so you can fit more data into an Index.
//   - Replicas: The number of [replicas] to use for the Index (defaults to 1). Replicas duplicate your Index.
//     They provide higher availability and throughput. Replicas can be scaled up or down as your needs change.
//   - SourceCollection: The name of the Collection to be used as the source for the Index.
//   - MetadataConfig: The [metadata configuration] for the behavior of Pinecone's internal metadata Index. By
//     default, all metadata is indexed; when `metadata_config` is present,
//     only specified metadata fields are indexed. These configurations are
//     only valid for use with pod-based Indexes.
//
// To create a new pods-based Index, use the CreatePodIndex method on the Client object.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    podIndexMetadata := &pinecone.PodSpecMetadataConfig{
//		       Indexed: &[]string{"title", "description"},
//	    }
//
//	    idx, err := pc.CreatePodIndex(ctx, &pinecone.CreatePodIndexRequest{
//	        Name:        "my-pod-index",
//	        Dimension:   3,
//	        Metric:      pinecone.Cosine,
//	        Environment: "us-west1-gcp",
//	        PodType:     "s1",
//	        MetadataConfig: podIndexMetadata,
//	        })
//
//	    if err != nil {
//		       log.Fatalf("Failed to create pod index: %v", err)
//	    } else {
//		       fmt.Printf("Successfully created pod index: %s", idx.Name)
//	    }
//
// [dimensionality]: https://docs.pinecone.io/guides/indexes/choose-a-pod-type-and-size#dimensionality-of-vectors
// [similarity]: https://docs.pinecone.io/guides/indexes/understanding-indexes#distance-metrics
// [metadata configuration]: https://docs.pinecone.io/guides/indexes/configure-pod-based-indexes#selective-metadata-indexing
// [cloud environment]: https://docs.pinecone.io/guides/indexes/understanding-indexes#pod-environments
// [replicas]: https://docs.pinecone.io/guides/indexes/configure-pod-based-indexes#add-replicas
//
// [type of pods]: https://docs.pinecone.io/guides/indexes/choose-a-pod-type-and-size
type CreatePodIndexRequest struct {
	Name             string
	Dimension        int32
	Metric           IndexMetric
	Environment      string
	PodType          string
	Shards           int32
	Replicas         int32
	SourceCollection *string
	MetadataConfig   *PodSpecMetadataConfig
}

// ReplicaCount ensures the replica count of a pods-based Index is >1.
// It returns a pointer to the number of replicas on a CreatePodIndexRequest object.
func (req CreatePodIndexRequest) ReplicaCount() *int32 {
	x := minOne(req.Replicas)
	return &x
}

// ShardCount ensures the number of shards on a pods-based Index is >1. It returns a pointer to the number of shards on
// a CreatePodIndexRequest object.
func (req CreatePodIndexRequest) ShardCount() *int32 {
	x := minOne(req.Shards)
	return &x
}

// TotalCount calculates and returns the total number of pods (replicas*shards) on a CreatePodIndexRequest object.
func (req CreatePodIndexRequest) TotalCount() *int {
	x := int(*req.ReplicaCount() * *req.ShardCount())
	return &x
}

// CreatePodIndex creates and initializes a new pods-based Index via the specified Client.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - in: A pointer to a CreatePodIndexRequest object. See CreatePodIndexRequest for more information.
//
// Returns a pointer to an Index object or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    podIndexMetadata := &pinecone.PodSpecMetadataConfig{
//		       Indexed: &[]string{"title", "description"},
//	    }
//
//	    idx, err := pc.CreatePodIndex(ctx, &pinecone.CreatePodIndexRequest{
//	        Name:        "my-pod-index",
//	        Dimension:   3,
//	        Metric:      pinecone.Cosine,
//	        Environment: "us-west1-gcp",
//	        PodType:     "s1",
//	        MetadataConfig: podIndexMetadata,
//	    })
//
//	    if err != nil {
//		       log.Fatalf("Failed to create pod index:", err)
//	    } else {
//		       fmt.Printf("Successfully created pod index: %s", idx.Name)
//	    }
func (c *Client) CreatePodIndex(ctx context.Context, in *CreatePodIndexRequest) (*Index, error) {
	metric := control.IndexMetric(in.Metric)
	req := control.CreateIndexRequest{
		Name:      in.Name,
		Dimension: in.Dimension,
		Metric:    &metric,
	}

	//add the spec to req.
	//because this is defined as an anon struct in the generated code, it must match exactly here.
	req.Spec = control.CreateIndexRequest_Spec{
		Pod: &struct {
			Environment    string `json:"environment"`
			MetadataConfig *struct {
				Indexed *[]string `json:"indexed,omitempty"`
			} `json:"metadata_config,omitempty"`
			PodType          control.PodSpecPodType   `json:"pod_type"`
			Pods             *int                     `json:"pods,omitempty"`
			Replicas         *control.PodSpecReplicas `json:"replicas,omitempty"`
			Shards           *control.PodSpecShards   `json:"shards,omitempty"`
			SourceCollection *string                  `json:"source_collection,omitempty"`
		}{
			Environment:      in.Environment,
			PodType:          in.PodType,
			Pods:             in.TotalCount(),
			Replicas:         in.ReplicaCount(),
			Shards:           in.ShardCount(),
			SourceCollection: in.SourceCollection,
		},
	}
	if in.MetadataConfig != nil {
		req.Spec.Pod.MetadataConfig = &struct {
			Indexed *[]string `json:"indexed,omitempty"`
		}{
			Indexed: in.MetadataConfig.Indexed,
		}
	}

	res, err := c.restClient.CreateIndex(ctx, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return nil, handleErrorResponseBody(res, "failed to create index: ")
	}

	return decodeIndex(res.Body)
}

// CreateServerlessIndexRequest holds the parameters for creating a new [Serverless] Index.
//
// Fields:
//   - Name: The name of the Index. Resource name must be 1-45 characters long,
//     start and end with an alphanumeric character,
//     and consist only of lower case alphanumeric characters or '-'.
//   - Dimension: The [dimensionality] of the vectors to be inserted in the Index.
//   - Metric: The metric used to measure the [similarity] between vectors ('euclidean', 'cosine', or 'dotproduct').
//   - Cloud: The public [cloud provider] where you would like your Index hosted.
//     For serverless Indexes, you define only the cloud and region where the Index should be hosted.
//   - Region: The [region] where you would like your Index to be created.
//
// To create a new Serverless Index, use the CreateServerlessIndex method on the Client object.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    idx, err := pc.CreateServerlessIndex(ctx, &pinecone.CreateServerlessIndexRequest{
//	        Name:    "my-serverless-index",
//	        Dimension: 3,
//	        Metric:  pinecone.Cosine,
//	        Cloud:   pinecone.Aws,
//	        Region:  "us-east-1",
//	    })
//
//	    if err != nil {
//	        log.Fatalf("Failed to create serverless index: %s", idx.Name)
//	    } else {
//	        fmt.Printf("Successfully created serverless index: %s", idx.Name)
//	    }
//
// [dimensionality]: https://docs.pinecone.io/guides/indexes/choose-a-pod-type-and-size#dimensionality-of-vectors
// [Serverless]: https://docs.pinecone.io/guides/indexes/understanding-indexes#serverless-indexes
// [similarity]: https://docs.pinecone.io/guides/indexes/understanding-indexes#distance-metrics
// [region]: https://docs.pinecone.io/troubleshooting/available-cloud-regions
// [cloud provider]: https://docs.pinecone.io/troubleshooting/available-cloud-regions#regions-available-for-serverless-indexes
type CreateServerlessIndexRequest struct {
	Name      string
	Dimension int32
	Metric    IndexMetric
	Cloud     Cloud
	Region    string
}

// CreateServerlessIndex creates and initializes a new serverless Index via the specified Client.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - in: A pointer to a CreateServerlessIndexRequest object. See CreateServerlessIndexRequest for more information.
//
// Returns a pointer to an Index object or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    idx, err := pc.CreateServerlessIndex(ctx, &pinecone.CreateServerlessIndexRequest{
//	        Name:    "my-serverless-index",
//	        Dimension: 3,
//	        Metric:  pinecone.Cosine,
//	        Cloud:   pinecone.Aws,
//	        Region:  "us-east-1",
//	    })
//
//	    if err != nil {
//	        log.Fatalf("Failed to create serverless index: %s", idx.Name)
//	    } else {
//	        fmt.Printf("Successfully created serverless index: %s", idx.Name)
//	    }
func (c *Client) CreateServerlessIndex(ctx context.Context, in *CreateServerlessIndexRequest) (*Index, error) {
	metric := control.IndexMetric(in.Metric)
	req := control.CreateIndexRequest{
		Name:      in.Name,
		Dimension: in.Dimension,
		Metric:    &metric,
		Spec: control.CreateIndexRequest_Spec{
			Serverless: &control.ServerlessSpec{
				Cloud:  control.ServerlessSpecCloud(in.Cloud),
				Region: in.Region,
			},
		},
	}

	res, err := c.restClient.CreateIndex(ctx, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return nil, handleErrorResponseBody(res, "failed to create index: ")
	}

	return decodeIndex(res.Body)
}

// DescribeIndex retrieves information about a specific Index. See Index for more information.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - idxName: The name of the Index to describe.
//
// Returns a pointer to an Index object or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    idx, err := pc.DescribeIndex(ctx, "the-name-of-my-index")
//	    if err != nil {
//	        log.Fatalf("Failed to describe index: %s", err)
//	    } else {
//	        fmt.Printf("%+v", *idx)
//	    }
func (c *Client) DescribeIndex(ctx context.Context, idxName string) (*Index, error) {
	res, err := c.restClient.DescribeIndex(ctx, idxName)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleErrorResponseBody(res, "failed to describe index: ")
	}

	return decodeIndex(res.Body)
}

// DeleteIndex deletes a specific Index.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - idxName: The name of the Index to delete.
//
// Returns an error if the deletion fails.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    indexName := "the-name-of-my-index"
//
//	    err = pc.DeleteIndex(ctx, indexName)
//	    if err != nil {
//		       log.Fatalf("Error: %v", err)
//	    } else {
//	        fmt.Printf("Index \"%s\" deleted successfully", indexName)
//	    }
func (c *Client) DeleteIndex(ctx context.Context, idxName string) error {
	res, err := c.restClient.DeleteIndex(ctx, idxName)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		return handleErrorResponseBody(res, "failed to delete index: ")
	}

	return nil
}

// ConfigureIndex is used to [scale a pods-based index] up or down by changing the size of the pods or the number of
// replicas.
//
// Parameters:
//   - name: The name of the index to configure.
//   - pods: (Optional) The pod size to scale the index to (e.g. for a "p1" pod type,
//     you could pass "p1.x2" to scale your index to the "x2" size,
//     or you could pass "p1.x4" to scale your index to the "x4" size, and
//     so forth.
//   - replicas: (Optional) The number of replicas to scale the index to.
//     This is capped by the maximum number of replicas allowed in your Pinecone project. To configure this number,
//     go to [app.pinecone.io], select your project, and configure the maximum number of pods.
//
// Note: You can only scale an index up, not down. If you want to scale an index down,
// you must create a new index with the desired configuration.
//
// Returns a pointer to a configured Index object or an error.
//
// Example for a pods-based index originally configured with 1 "p1" pod of size "x2" and 1 replica:
//
//	// To scale the size of your pods from "x2" to "x4":
//	 _, err := pc.ConfigureIndex(ctx, "my-index", "p1.x4", nil)
//	 if err != nil {
//	     fmt.Printf("Failed to configure index: %v\n", err)
//	 }
//
//	// To scale the number of replicas:
//	 _, err := pc.ConfigureIndex(ctx, "my-index", nil, 4)
//	 if err != nil {
//	     fmt.Printf("Failed to configure index: %v\n", err)
//	 }
//
//	// To scale both the size of your pods and the number of replicas:
//	 _, err := pc.ConfigureIndex(ctx, "my-index", "p1.x4", 4)
//	 if err != nil {
//	     fmt.Printf("Failed to configure index: %v\n", err)
//	 }
//
// [scale a pods-based index]: https://docs.pinecone.io/guides/indexes/configure-pod-based-indexes
// [app.pinecone.io]: https://app.pinecone.io
func (c *Client) ConfigureIndex(ctx context.Context, name string, podType *string,
	replicas *int32) (*Index, error) {

	if podType == nil && replicas == nil {
		return nil, fmt.Errorf("must specify either podType or replicas")
	}

	request := control.ConfigureIndexRequest{
		Spec: struct {
			Pod struct {
				PodType  *control.PodSpecPodType  `json:"pod_type,omitempty"`
				Replicas *control.PodSpecReplicas `json:"replicas,omitempty"`
			} `json:"pod"`
		}{
			Pod: struct {
				PodType  *control.PodSpecPodType  `json:"pod_type,omitempty"`
				Replicas *control.PodSpecReplicas `json:"replicas,omitempty"`
			}{
				PodType:  podType,
				Replicas: replicas,
			},
		},
	}

	res, err := c.restClient.ConfigureIndex(ctx, name, request)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleErrorResponseBody(res, "failed to configure index: ")
	}

	return decodeIndex(res.Body)
}

// ListCollections retrieves a list of all Collections in a Pinecone [project]. See Collection for more information.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//
// Returns a slice of pointers to [Collection] objects or an error.
//
// Note: Collections are only available for pods-based Indexes.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    collections, err := pc.ListCollections(ctx)
//	    if err != nil {
//		       log.Fatalf("Failed to list collections: %v", err)
//	    } else {
//		       if len(collections) == 0 {
//		           fmt.Printf("No collections found in project")
//		       } else {
//		           fmt.Println("Collections in project:")
//		           for _, collection := range collections {
//			           fmt.Printf("- %s\n", collection.Name)
//		           }
//		       }
//	    }
//
// [project]: https://docs.pinecone.io/guides/projects/understanding-projects
// [Collection]: https://docs.pinecone.io/guides/indexes/understanding-collections
func (c *Client) ListCollections(ctx context.Context) ([]*Collection, error) {
	res, err := c.restClient.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleErrorResponseBody(res, "failed to list collections: ")
	}

	var collectionsResponse control.CollectionList
	if err := json.NewDecoder(res.Body).Decode(&collectionsResponse); err != nil {
		return nil, err
	}

	var collections []*Collection
	for _, collectionModel := range *collectionsResponse.Collections {
		collections = append(collections, toCollection(&collectionModel))
	}

	return collections, nil
}

// DescribeCollection retrieves information about a specific [Collection].
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - collectionName: The name of the Collection to describe.
//
// Returns a pointer to a Collection object or an error.
//
// Note: Collections are only available for pods-based Indexes.
//
// Since the returned value is a pointer to a Collection object, it will have the following fields:
//   - Name: The name of the Collection.
//   - Size: The size of the Collection in bytes.
//   - Status: The status of the Collection.
//   - Dimension: The [dimensionality] of the vectors stored in each record held in the Collection.
//   - VectorCount: The number of records stored in the Collection.
//   - Environment: The cloud environment where the Collection is hosted.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    collection, err := pc.DescribeCollection(ctx, "my-collection")
//	    if err != nil {
//		       log.Fatalf("Error describing collection: %v", err)
//	    } else {
//		       fmt.Printf("Collection: %+v\n", *collection)
//	    }
//
// [dimensionality]: https://docs.pinecone.io/guides/indexes/choose-a-pod-type-and-size#dimensionality-of-vectors
// [Collection]: https://docs.pinecone.io/guides/indexes/understanding-collections
func (c *Client) DescribeCollection(ctx context.Context, collectionName string) (*Collection, error) {
	res, err := c.restClient.DescribeCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleErrorResponseBody(res, "failed to describe collection: ")
	}

	return decodeCollection(res.Body)
}

// CreateCollectionRequest holds the parameters for creating a new [Collection].
//
// Fields:
//   - Name: The name of the Collection.
//   - Source: The name of the Index to be used as the source for the Collection.
//
// To create a new Collection, use the CreateCollection method on the Client object.
//
// Note: Collections are only available for pods-based Indexes.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    collection, err := pc.CreateCollection(ctx, &pinecone.CreateCollectionRequest{
//	        Name:   "my-collection",
//	        Source: "my-source-index",
//	     })
//	    if err != nil {
//		       log.Fatalf("Failed to create collection: %v", err)
//	    } else {
//		       fmt.Printf("Successfully created collection \"%s\".", collection.Name)
//	    }
//
// [Collection]: https://docs.pinecone.io/guides/indexes/understanding-collections
type CreateCollectionRequest struct {
	Name   string
	Source string
}

// CreateCollection creates and initializes a new [Collection] via the specified Client.
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - in: A pointer to a CreateCollectionRequest object.
//
// Note: Collections are only available for pods-based Indexes.
//
// Returns a pointer to a Collection object or an error.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    collection, err := pc.CreateCollection(ctx, &pinecone.CreateCollectionRequest{
//	        Name:   "my-collection",
//	        Source: "my-source-index",
//	    })
//	    if err != nil {
//		       log.Fatalf("Failed to create collection: %v", err)
//	    } else {
//		       fmt.Printf("Successfully created collection \"%s\".", collection.Name)
//	    }
//
// [Collection]: https://docs.pinecone.io/guides/indexes/understanding-collections
func (c *Client) CreateCollection(ctx context.Context, in *CreateCollectionRequest) (*Collection, error) {
	req := control.CreateCollectionRequest{
		Name:   in.Name,
		Source: in.Source,
	}
	res, err := c.restClient.CreateCollection(ctx, req)

	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return nil, handleErrorResponseBody(res, "failed to create collection: ")
	}

	return decodeCollection(res.Body)
}

// DeleteCollection deletes a specific [Collection]
//
// Parameters:
//   - ctx: A context.Context object controls the request's lifetime, allowing for the request
//     to be canceled or to timeout according to the context's deadline.
//   - collectionName: The name of the Collection to delete.
//
// Note: Collections are only available for pods-based Indexes.
//
// Returns an error if the deletion fails.
//
// Example:
//
//	    ctx := context.Background()
//
//	    clientParams := pinecone.NewClientParams{
//		       ApiKey:    "YOUR_API_KEY",
//		       SourceTag: "your_source_identifier", // optional
//	    }
//
//	    pc, err := pinecone.NewClient(clientParams)
//	    if err != nil {
//	        log.Fatalf("Failed to create Client: %v", err)
//	    } else {
//		       fmt.Println("Successfully created a new Client object!")
//	    }
//
//	    collectionName := "my-collection"
//
//	    err = pc.DeleteCollection(ctx, collectionName)
//	    if err != nil {
//		       log.Fatalf("Failed to create collection: %s\n", err)
//	    } else {
//		       log.Printf("Successfully deleted collection \"%s\"\n", collectionName)
//	    }
//
// [Collection]: https://docs.pinecone.io/guides/indexes/understanding-collections
func (c *Client) DeleteCollection(ctx context.Context, collectionName string) error {
	res, err := c.restClient.DeleteCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		return handleErrorResponseBody(res, "failed to delete collection: ")
	}

	return nil
}

func (c *Client) extractAuthHeader() map[string]string {
	possibleAuthKeys := []string{
		"api-key",
		"authorization",
		"access_token",
	}

	for key, value := range c.headers {
		for _, checkKey := range possibleAuthKeys {
			if strings.ToLower(key) == checkKey {
				return map[string]string{key: value}
			}
		}
	}

	return nil
}

func toIndex(idx *control.IndexModel) *Index {
	if idx == nil {
		return nil
	}

	spec := &IndexSpec{}
	if idx.Spec.Pod != nil {
		spec.Pod = &PodSpec{
			Environment:      idx.Spec.Pod.Environment,
			PodType:          idx.Spec.Pod.PodType,
			PodCount:         int32(idx.Spec.Pod.Pods),
			Replicas:         idx.Spec.Pod.Replicas,
			ShardCount:       idx.Spec.Pod.Shards,
			SourceCollection: idx.Spec.Pod.SourceCollection,
		}
		if idx.Spec.Pod.MetadataConfig != nil {
			spec.Pod.MetadataConfig = &PodSpecMetadataConfig{Indexed: idx.Spec.Pod.MetadataConfig.Indexed}
		}
	}
	if idx.Spec.Serverless != nil {
		spec.Serverless = &ServerlessSpec{
			Cloud:  Cloud(idx.Spec.Serverless.Cloud),
			Region: idx.Spec.Serverless.Region,
		}
	}
	status := &IndexStatus{
		Ready: idx.Status.Ready,
		State: IndexStatusState(idx.Status.State),
	}
	return &Index{
		Name:      idx.Name,
		Dimension: idx.Dimension,
		Host:      idx.Host,
		Metric:    IndexMetric(idx.Metric),
		Spec:      spec,
		Status:    status,
	}
}

func decodeIndex(resBody io.ReadCloser) (*Index, error) {
	var idx control.IndexModel
	err := json.NewDecoder(resBody).Decode(&idx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode idx response: %w", err)
	}

	return toIndex(&idx), nil
}

func toCollection(cm *control.CollectionModel) *Collection {
	if cm == nil {
		return nil
	}

	return &Collection{
		Name:        cm.Name,
		Size:        derefOrDefault(cm.Size, 0),
		Status:      CollectionStatus(cm.Status),
		Dimension:   derefOrDefault(cm.Dimension, 0),
		VectorCount: derefOrDefault(cm.VectorCount, 0),
		Environment: cm.Environment,
	}
}

func decodeCollection(resBody io.ReadCloser) (*Collection, error) {
	var collectionModel control.CollectionModel
	err := json.NewDecoder(resBody).Decode(&collectionModel)
	if err != nil {
		return nil, fmt.Errorf("failed to decode collection response: %w", err)
	}

	return toCollection(&collectionModel), nil
}

func decodeErrorResponse(resBodyBytes []byte) (*control.ErrorResponse, error) {
	var errorResponse control.ErrorResponse
	err := json.Unmarshal(resBodyBytes, &errorResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode error response: %w", err)
	}

	if errorResponse.Status == 0 {
		return nil, fmt.Errorf("unable to parse ErrorResponse: %v", string(resBodyBytes))
	}

	return &errorResponse, nil
}

type errorResponseMap struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Details    string `json:"details,omitempty"`
}

func handleErrorResponseBody(response *http.Response, errMsgPrefix string) error {
	resBodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var errMap errorResponseMap
	errMap.StatusCode = response.StatusCode

	// try and decode ErrorResponse
	if json.Valid(resBodyBytes) {
		errorResponse, err := decodeErrorResponse(resBodyBytes)
		if err == nil {
			errMap.Message = errorResponse.Error.Message
			errMap.ErrorCode = string(errorResponse.Error.Code)

			if errorResponse.Error.Details != nil {
				errMap.Details = fmt.Sprintf("%+v", errorResponse.Error.Details)
			}
		}
	}

	errMap.Body = string(resBodyBytes)

	if errMap.Message != "" {
		errMap.Message = errMsgPrefix + errMap.Message
	}

	return formatError(errMap)
}

func formatError(errMap errorResponseMap) error {
	jsonString, err := json.Marshal(errMap)
	if err != nil {
		return err
	}
	baseError := fmt.Errorf(string(jsonString))

	return &PineconeError{Code: errMap.StatusCode, Msg: baseError}
}

func buildClientBaseOptions(in NewClientBaseParams) []control.ClientOption {
	clientOptions := []control.ClientOption{}

	// build and apply user agent
	userAgentProvider := provider.NewHeaderProvider("User-Agent", useragent.BuildUserAgent(in.SourceTag))
	clientOptions = append(clientOptions, control.WithRequestEditorFn(userAgentProvider.Intercept))

	envAdditionalHeaders, hasEnvAdditionalHeaders := os.LookupEnv("PINECONE_ADDITIONAL_HEADERS")
	additionalHeaders := make(map[string]string)

	// add headers from environment
	if hasEnvAdditionalHeaders {
		err := json.Unmarshal([]byte(envAdditionalHeaders), &additionalHeaders)
		if err != nil {
			log.Printf("failed to parse PINECONE_ADDITIONAL_HEADERS: %v", err)
		}
	}

	// merge headers from parameters if passed
	if in.Headers != nil {
		for key, value := range in.Headers {
			additionalHeaders[key] = value
		}
	}

	// add headers to client options
	for key, value := range additionalHeaders {
		headerProvider := provider.NewHeaderProvider(key, value)
		clientOptions = append(clientOptions, control.WithRequestEditorFn(headerProvider.Intercept))
	}

	// apply custom http client if provided
	if in.RestClient != nil {
		clientOptions = append(clientOptions, control.WithHTTPClient(in.RestClient))
	}

	return clientOptions
}

func ensureURLScheme(inputURL string) (string, error) {
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %v", err)
	}

	if parsedURL.Scheme == "" {
		return "https://" + inputURL, nil
	}
	return inputURL, nil
}

func valueOrFallback[T comparable](value, fallback T) T {
	var zero T // set to zero-value of generic type T
	if value != zero {
		return value
	} else {
		return fallback
	}
}

func derefOrDefault[T any](ptr *T, defaultValue T) T {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

func minOne(x int32) int32 {
	if x < 1 { // ensure x is at least 1
		return 1
	}
	return x
}
