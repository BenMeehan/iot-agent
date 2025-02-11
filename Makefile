name: CI Pipeline

on:
  push:
    branches:
      - main
  pull_request:
    branches:
      - main

jobs:
  build:
    runs-on: ubuntu-latest

    steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.23'

    - name: Install dependencies
      run: make deps

    - name: Run linting
      run: make lint

    - name: Run tests
      run: make test

    - name: Build project
      run: make build

    - name: Upload build artifacts
      uses: actions/upload-artifact@v4
      with:
        name: agent
        path: bin/agent