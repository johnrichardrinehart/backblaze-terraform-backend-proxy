# Introduction

`terraform` currently has no support for Backblaze as a state backend. However, `terraform` *does* support remote state 
management using an HTTP server. So, this project looks to set up an interface between `terraform` and Backblaze (`B2`) 
using an HTTP server that handles processing state updates from `terraform` and fetching state from `B2`.

# Getting Started

1. Create a `B2` bucket - make sure to enable object locking (if you want to use `terraform` state locking)
1. Create an application key (see note below for why the default master key can not be used)
1. Start the server as `B2_KEY_ID=$B2_KEY_ID B2_APP_KEY=$SUPER_SECRET_ACCESS_KEY ./server`
1. Use `terraform` as usual

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

# Problems I had with Backblaze

So, below is a laundry list of issues I encountered when trying to develop this service. Some of them are documentation issues, some of them are UI issues, and some of them are best practice issues.

1. The "native" API seems more like a legacy API. Object locking is not supported with the "native" API but *is* through the S3 API. The idea of a Backblaze-first API is great, but if it lacks features relative to a competitor's API then just adopt one. Probably the "native" API still exists for compatibility reasons (customers still use it). But, then start a deprecation process, help your customers migrate, make the current limitations of the native API very clear, and invest all engineering and documentation into the S3 API. To be fair, they have [addressed future plans to implement object locking in the Native API](https://help.backblaze.com/hc/en-us/articles/360052973274-Object-Lock-FAQs?_ga=2.116537154.575951376.1616680733-785951114.1616322351), but there's no timeline and, again, it feels like API effortst are being spread wider than they are deep. I feel like a good solution would be to create an open standard/API for buckets based on S3 and implement this open standard. Backblaze has some community so this could be a powerful shift toward openness in Cloud standards.
1. The documentation for the S3 API is pretty sparse, including which region should be used when using the AWS SDK. You can find demo code [here](https://help.backblaze.com/hc/en-us/articles/360047629713?_ga=2.46341987.575951376.1616680733-785951114.1616322351) but there is no code formatting, syntax highlighting, and that code didn't even work. About that last point: After struggling with authentication issues for a while I discovered that the host provided is the wrong one (even though it looks right). I had to parse the host from a *screenshot* on another [page](https://help.backblaze.com/hc/en-us/articles/360047779633-Configuring-the-AWS-CLI-for-use-with-B2) documenting how to use the AWS CLI (not the Go SDK) which you might not even guess is the right host to use, because the only difference between that host and the one on the article/blog post about the Go SDK seems to be the AWS region part of the hostname, and:
   - no region *needs* to be specified with the AWS CLI when using a Backblaze host
   - the region in the `endpoint` host name of the demo code *matched* the region used in the `aws.Config` struct, so I was biased to think it was the *right* hostname
   - the error I was getting was a `403` error claiming my "`key '01234...abcd...000' is not valid`", which you probably wouldn't guess would be fixed by changing the `endpoint`...
1. The documentation is really hard to read and navigate
    - The native HTTP API doesn't specify what HTTP method to use when making a request (technically, if you inspect the Java/Python/Curl examples you can parse it out)
    - The demo code doesn't use a monospace/code font, it's not syntax-highlighted, it's not indent properly. It's not even idiomatic Go code (`err.Error()` when printing a formatted string?), so it's really hard to read.
    - Some example code snippets use images (https://help.backblaze.com/hc/article_attachments/360069634513/image-0.png) instead of text, so you can't copy-paste
1. The web UI to view files in the bucket is slow to update the content. Page refreshes, B2 portal navigations, traversing bucket filesystem depths (jump up/down a level) doesn't seem to help. It seems to take at least 20s to register changes, but it's very unpredictable (could take a minute).