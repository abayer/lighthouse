#!/usr/bin/env bash
set -e
set -x

export BB_USERNAME="jenkins-x-bdd"
export BB_OWNER="jxbdd"
export BB_EMAIL="jenkins-x@googlegroups.com"

# fix broken `BUILD_NUMBER` env var
export BUILD_NUMBER="$BUILD_ID"

JX_HOME="/tmp/jxhome"
KUBECONFIG="/tmp/jxhome/config"

# lets avoid the git/credentials causing confusion during the test
export XDG_CONFIG_HOME=$JX_HOME

mkdir -p $JX_HOME/git

jx --version

# replace the credentials file with a single user entry
echo "https://$BB_USERNAME:$BB_ACCESS_TOKEN@bitbucket.beescloud.com" > $JX_HOME/git/credentials

gcloud auth activate-service-account --key-file $GKE_SA

# lets setup git 
git config --global --add user.name JenkinsXBot
git config --global --add user.email jenkins-x@googlegroups.com

echo "running the BDD tests with JX_HOME = $JX_HOME"

# setup jx boot parameters
export JX_VALUE_ADMINUSER_PASSWORD="$JENKINS_PASSWORD"
export JX_VALUE_PIPELINEUSER_USERNAME="$BB_USERNAME"
export JX_VALUE_PIPELINEUSER_EMAIL="$BB_EMAIL"
export JX_VALUE_PIPELINEUSER_TOKEN="$BB_ACCESS_TOKEN"
export JX_VALUE_PROW_HMACTOKEN="$BB_ACCESS_TOKEN"

# TODO temporary hack until the batch mode in jx is fixed...
export JX_BATCH_MODE="true"

# Push the snapshot chart
pushd charts/lighthouse
make snapshot
popd

# Use the latest boot config promoted in the version stream instead of master to avoid conflicts during boot, because
# boot fetches always the latest version available in the version stream.
git clone https://github.com/jenkins-x/jenkins-x-versions.git versions
export BOOT_CONFIG_VERSION=$(jx step get dependency-version --host=github.com --owner=jenkins-x --repo=jenkins-x-boot-config --dir versions | sed 's/.*: \(.*\)/\1/')
git clone https://github.com/jenkins-x/jenkins-x-boot-config.git boot-source
cd boot-source
git checkout tags/v${BOOT_CONFIG_VERSION} -b latest-boot-config

cp ../bdd/bbs/jx-requirements.yml .
cp ../bdd/bbs/parameters.yaml env

# Manually interpolate lighthouse version tag
cat ../bdd/values.yaml.template >> env/lighthouse/values.tmpl.yaml
cp env/lighthouse/values.tmpl.yaml values.tmpl.yaml.tmp
sed 's/$VERSION/'"$VERSION"'/' values.tmpl.yaml.tmp > env/lighthouse/values.tmpl.yaml
cat env/lighthouse/values.tmpl.yaml
rm values.tmpl.yaml.tmp
sed -e s/\$VERSION/${VERSION}/g ../bdd/helm-requirements.yaml.template > env/requirements.yaml

# append to the existing jenkins-x-platform values.tmpl.yaml to disable build controller status reporting
cat ../bdd/platform-values.yaml.template >> env/jenkins-x-platform/values.tmpl.yaml

# TODO: Disable chatops tests until issue creation and labeling on BBS is ready
export BDD_ENABLE_TEST_CHATOPS_COMMANDS="false"

echo "Building lighthouse with version $VERSION"

# TODO hack until we fix boot to do this too!
helm init --client-only
helm repo add jenkins-x https://storage.googleapis.com/chartmuseum.jenkins-x.io

# TODO: Re-enable checking whether build controller has updated PipelineActivity once BBS is actually being worked on again.
export BDD_DISABLE_PIPELINEACTIVITY_CHECK=true

set +e
jx step bdd \
    --versions-repo https://github.com/jenkins-x/jenkins-x-versions.git \
    --config ../bdd/bbs/cluster.yaml \
    --gopath /tmp \
    --git-provider bitbucketserver \
    --git-provider-url https://bitbucket.beescloud.com \
    --git-owner $BB_OWNER \
    --git-username $BB_USERNAME \
    --git-api-token $BB_ACCESS_TOKEN \
    --default-admin-password $JENKINS_PASSWORD \
    --no-delete-app \
    --no-delete-repo \
    --tests install \
    --tests test-create-spring

# Bitbucket doesn't have pull request labels, so...yeah. Can't do quickstart tests, to say the least.

bdd_result=$?
if [[ $bdd_result != 0 ]]; then
  pushd ..
  mkdir -p extra-logs
  kubectl logs --tail=-1 "$(kubectl get pod -l app=controllerbuild -o jsonpath='{.items[*].metadata.name}')" > extra-logs/controllerbuild.log
  kubectl logs --tail=-1 "$(kubectl get pod -l app=lighthouse-keeper -o jsonpath='{.items[*].metadata.name}')" > extra-logs/keeper.log
  kubectl logs --tail=-1 "$(kubectl get pod -l app=lighthouse-foghorn -o jsonpath='{.items[*].metadata.name}')" > extra-logs/foghorn.log
  lh_cnt=0
  for lh_pod in $(kubectl get pod -l app=lighthouse-webhooks -o jsonpath='{.items[*].metadata.name}'); do
    ((lh_cnt=lh_cnt+1))
    kubectl logs --tail=-1 "${lh_pod}" > extra-logs/lh.${lh_cnt}.log
  done

  jx step stash -c lighthouse-tests -p "extra-logs/*.log" --bucket-url gs://jx-prod-logs
  popd
fi
cd ../charts/lighthouse
make delete-from-chartmuseum

exit $bdd_result
