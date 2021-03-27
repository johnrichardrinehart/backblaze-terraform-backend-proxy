# Introduction

`terraform` currently has no support for Backblaze as a state backend. However, `terraform` *does* support remote state 
management using an HTTP server. So, this project looks to set up an interface between `terraform` and Backblaze (`B2`) 
using an HTTP server that handles processing state updates from `terraform` and fetching state from `B2`.

# Note about Locking

`terraform` supports locking state files in order to prevent concurrent writes. `B2` also supports locking files/objects
within a bucket, but only using its `S3`-compatible API. The native HTTP API of Backblaze doesn't support it. So,
internally this project uses the official [AWS Go SDK for S3](https://pkg.go.dev/github.com/aws/aws-sdk-go/service/s3).


Please note the following regarding `B2` security from the Backblaze
[official documentation](https://www.backblaze.com/b2/docs/s3_compatible_api.html):

> The automatically created Master Application Key is not supported in the Backblaze S3 Compatible API - only
Application Keys that are manually created in the Backblaze Web UI or via the Backblaze B2 Native API can be used to
authenticate the Backblaze S3 Compatible API.

So, make sure to create an application key that can be used with this integration.