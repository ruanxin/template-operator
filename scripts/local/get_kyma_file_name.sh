#!/bin/bash

function get_kyma_file_name () {
	_OS_TYPE="$(uname -s)"
  _OS_ARCH="$(uname -m)"

	[ "$_OS_TYPE" == "Linux"   ] && [ "$_OS_ARCH" == "x86_64" ] && echo "kyma-linux"     ||
	[ "$_OS_TYPE" == "Linux"   ] && [ "$_OS_ARCH" == "arm64"  ] && echo "kyma-linux-arm" ||
	[ "$_OS_TYPE" == "Windows" ] && [ "$_OS_ARCH" == "x86_64" ] && echo "kyma.exe"       ||
	[ "$_OS_TYPE" == "Windows" ] && [ "$_OS_ARCH" == "arm64"  ] && echo "kyma-arm.exe"   ||
	[ "$_OS_TYPE" == "Darwin"  ] && [ "$_OS_ARCH" == "x86_64" ] && echo "kyma-darwin"
}

get_kyma_file_name
