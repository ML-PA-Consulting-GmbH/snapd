#!/bin/bash
echo "Building snapd binaries.."

cd "$(dirname "$0")" || exit 1
BASEPATH=$PWD
BINPATH=$BASEPATH/bin
mkdir -p "${BINPATH}"
rm -rf "${BINPATH:?}/*"
mkdir -p "${BINPATH}/amd64"
mkdir -p "${BINPATH}/arm64"

for COMPILE_PACKAGE in snapd snap-seccomp snapctl snap-fde-keymgr snap-preseed snap-repair snap-update-ns snap-exec snap-bootstrap snap-failure snap-chooser
do
    for COMPILE_ARCH in amd64 arm64
    do
        echo "Building ${COMPILE_PACKAGE}/${COMPILE_ARCH}.."
        cd "${BASEPATH}/cmd/${COMPILE_PACKAGE}" || exit 1
        env GOOS=linux GOARCH=$COMPILE_ARCH go build -o "${BINPATH}/${COMPILE_ARCH}/${COMPILE_PACKAGE}"
    done
done

#cd "${BASEPATH}/cmd/snapd" || exit 1
#go build -o "${BINPATH}/snapd"
#
#cd "${BASEPATH}/cmd/snap-seccomp" || exit 1
#go build -o "${BINPATH}/snap-seccomp"
#
#cd "${BASEPATH}/cmd/snapctl" || exit 1
#go build -o "${BINPATH}/snapctl"
#
#cd "${BASEPATH}/cmd/snap-fde-keymgr" || exit 1
#go build -o "${BINPATH}/snap-fde-keymgr"
#
#cd "${BASEPATH}/cmd/snap-preseed" || exit 1
#go build -o "${BINPATH}/snap-preseed"
#
#cd "${BASEPATH}/cmd/snap-repair" || exit 1
#go build -o "${BINPATH}/snap-repair"
#
#cd "${BASEPATH}/cmd/snap-update-ns" || exit 1
#go build -o "${BINPATH}/snap-update-ns"
#
#cd "${BASEPATH}/cmd/snap-exec" || exit 1
#go build -o "${BINPATH}/snap-exec"
#
#cd "${BASEPATH}/cmd/snap-bootstrap" || exit 1
#go build -o "${BINPATH}/snap-bootstrap"
#
#cd "${BASEPATH}/cmd/snap-failure" || exit 1
#go build -o "${BINPATH}/snap-failure"
#
#cd "${BASEPATH}/cmd/snap-chooser" || exit 1
#go build -o "${BINPATH}/snap-chooser"

echo "Finished building binaries:"
cd "${BASEPATH}" || exit 1
ls -la bin/*