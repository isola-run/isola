kubectl create namespace localstack
helm repo add localstack https://localstack.github.io/helm-charts
helm repo update
helm install localstack localstack/localstack -n localstack
kubectl -n localstack port-forward svc/localstack 4566:4566


aws --endpoint-url=http://localhost:4566 s3api create-bucket \
  --bucket isola-uploads \
  --region us-east-1


export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1

