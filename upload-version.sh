#!/bin/bash

git fetch --tags

last_version=$(git describe --tags `git rev-list --tags --max-count=1`)
base=$(echo $last_version | cut -d '.' -f1-2)
minor=$(echo $last_version | cut -d '.' -f3)
new_minor=$((minor + 1))
new_version="${base}.${new_minor}"

echo "Creating new version: $new_version"

git tag "$new_version"
sed -i "s/appVersion: .*/appVersion: \"$new_version\"/" charts/Chart.yaml
git add charts/Chart.yaml
git commit -m "Update appVersion to $new_version"

git push origin main
git push --tags
