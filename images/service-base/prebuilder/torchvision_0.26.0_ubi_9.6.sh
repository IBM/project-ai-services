#!/bin/bash -e
#
# -----------------------------------------------------------------------------
#
# Package           : vision
# Version           : v0.26.0
# Source repo       : https://github.com/pytorch/vision.git
# Tested on         : UBI:9.6
# Language          : Python
# Ci-Check      : True
# Script License    : Apache License, Version 2.0
# Maintainer        : Ira <ira.pandey1@ibm.com>
#
# Disclaimer        : This script has been tested in root mode on given
# ==========          platform using the mentioned version of the package.
#                     It may not work as expected with newer versions of the
#                     package and/or distribution. In such case, please
#                     contact "Maintainer" of this script.
#
# ----------------------------------------------------------------------------

set -ex 

PACKAGE_NAME=vision
PACKAGE_VERSION=${1:-v0.29.0}
PACKAGE_URL=https://github.com/pytorch/vision.git
OS_NAME=$(cat /etc/os-release | grep ^PRETTY_NAME | cut -d= -f2)
MAX_JOBS=${MAX_JOBS:-$(nproc)}
VERSION=${PACKAGE_VERSION#v}
# The wrapper (create_wheel_wrapper.sh) passes the Python version (eg: 3.12) as $2.
# Accept $2 as the PyTorch tag only if it looks like one (eg: v2.14.0).
PYTORCH_VERSION_ARG=${2:-}
[[ "$PYTORCH_VERSION_ARG" =~ ^v[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || PYTORCH_VERSION_ARG=""
if [[ "$PACKAGE_VERSION" == "v0.29.0" ]]; then
    PYTORCH_VERSION=${PYTORCH_VERSION_ARG:-v2.14.0}
elif [[ "$PACKAGE_VERSION" == "v0.28.0" ]]; then
    PYTORCH_VERSION=${PYTORCH_VERSION_ARG:-v2.13.0}
elif [[ "$PACKAGE_VERSION" == "v0.27.1" ]]; then
    PYTORCH_VERSION=${PYTORCH_VERSION_ARG:-v2.12.1}
elif [[ "$PACKAGE_VERSION" == "v0.27.0" ]]; then
    PYTORCH_VERSION=${PYTORCH_VERSION_ARG:-v2.12.0}
else
    PYTORCH_VERSION=${PYTORCH_VERSION_ARG:-v2.11.0}
fi

CURRENT_DIR=$(pwd)

# Install Python 3.12 explicitly (instead of the default python3/python3-devel which resolves
# to 3.9 on UBI 9) so the entire build uses a consistent 3.12 interpreter.
yum install -y git make wget python3.12 python3.12-devel python3.12-pip pkgconfig atlas libjpeg-devel openblas-devel
yum install gcc-toolset-13 -y
yum install -y make libtool  xz zlib-devel openssl-devel bzip2-devel libffi-devel libevent-devel  patch ninja-build gcc-toolset-13  pkg-config  gmp-devel  freetype-devel

# Point the generic python/pip commands to 3.12 so all subsequent invocations
# (pip install, python3 -m pip, etc.) use the correct interpreter.
ln -sf /usr/bin/pip3.12 /usr/bin/pip && ln -sf /usr/bin/python3.12 /usr/bin/python

export PYTHON_SITE_PACKAGES=$(python - <<'EOF'
import sysconfig, os

venv = os.environ.get("VIRTUAL_ENV")
paths = sysconfig.get_paths()

if venv:
    # Prefer venv path
    print(paths["platlib"])
else:
    # System python
    print(paths["platlib"])
EOF
)

dnf install -y gcc-toolset-13-libatomic-devel

export PATH=/opt/rh/gcc-toolset-13/root/usr/bin:$PATH
export LD_LIBRARY_PATH=/opt/rh/gcc-toolset-13/root/usr/lib64:$LD_LIBRARY_PATH

echo "------------Installing cmake---------------------------"

echo "Installing cmake..."
pip install cmake
cd $CURRENT_DIR

echo "---------------------openblas installing---------------------"

#install openblas
#clone and install openblas from source

git clone https://github.com/OpenMathLib/OpenBLAS
cd OpenBLAS
git checkout v0.3.29
git submodule update --init

# Set build options
declare -a build_opts
# Fix ctest not automatically discovering tests
LDFLAGS=$(echo "${LDFLAGS}" | sed "s/-Wl,--gc-sections//g")
export CF="${CFLAGS} -Wno-unused-parameter -Wno-old-style-declaration"
unset CFLAGS
export USE_OPENMP=1
build_opts+=(USE_OPENMP=${USE_OPENMP})
export PREFIX=${PREFIX}

# Handle Fortran flags
if [ ! -z "$FFLAGS" ]; then
    export FFLAGS="${FFLAGS/-fopenmp/ }"
    export FFLAGS="${FFLAGS} -frecursive"
    export LAPACK_FFLAGS="${FFLAGS}"
fi
export PLATFORM=$(uname -m)
build_opts+=(BINARY="64")
build_opts+=(DYNAMIC_ARCH=1)
build_opts+=(TARGET="POWER9")
BUILD_BFLOAT16=1

# Placeholder for future builds that may include ILP64 variants.
build_opts+=(INTERFACE64=0)
build_opts+=(SYMBOLSUFFIX="")

# Build LAPACK
build_opts+=(NO_LAPACK=0)

# Enable threading and set the number of threads
build_opts+=(USE_THREAD=1)
build_opts+=(NUM_THREADS=8)

# Disable CPU/memory affinity handling to avoid problems with NumPy and R
build_opts+=(NO_AFFINITY=1)

# Build OpenBLAS
make -j8 ${build_opts[@]} CFLAGS="${CF}" FFLAGS="${FFLAGS}" prefix=${PREFIX}

# Install OpenBLAS
CFLAGS="${CF}" FFLAGS="${FFLAGS}" make install PREFIX="${PREFIX}" ${build_opts[@]}
OpenBLASInstallPATH=$(pwd)/$PREFIX
OpenBLASConfigFile=$(find . -name OpenBLASConfig.cmake)
OpenBLASPCFile=$(find . -name openblas.pc)
export LD_LIBRARY_PATH="$OpenBLASInstallPATH/lib":${LD_LIBRARY_PATH}
export PKG_CONFIG_PATH="$OpenBLASInstallPATH/lib/pkgconfig:${PKG_CONFIG_PATH}"
export LD_LIBRARY_PATH=${PREFIX}/lib:$LD_LIBRARY_PATH
export PKG_CONFIG_PATH=${PREFIX}/lib/pkgconfig:$PKG_CONFIG_PATH
pkg-config --modversion openblas
cd $CURRENT_DIR

echo "--------------------scipy installing-------------------------------"

# Install scipy from the devpi index instead of building from source.
python3 -m pip install scipy==1.15.2 \
    --extra-index-url=https://wheels.developerfirst.ibm.com/ppc64le/linux/ \
    --prefer-binary

echo "--------------------abseil-cpp installing-------------------------------"

#cloning abseil-cpp
ABSEIL_VERSION=20240116.2
ABSEIL_URL="https://github.com/abseil/abseil-cpp"

git clone $ABSEIL_URL -b $ABSEIL_VERSION

echo "------------abseil-cpp cloned--------------"

#building libprotobuf
export C_COMPILER=$(which gcc)
export CXX_COMPILER=$(which g++)

echo "----------------protobuf installing-------------------"
git clone https://github.com/protocolbuffers/protobuf
cd protobuf
git checkout v4.25.8

LIBPROTO_DIR=$(pwd)
mkdir -p $LIBPROTO_DIR/local/libprotobuf
LIBPROTO_INSTALL=$LIBPROTO_DIR/local/libprotobuf

git submodule update --init --recursive
rm -rf ./third_party/googletest | true
rm -rf ./third_party/abseil-cpp | true

cp -r $CURRENT_DIR/abseil-cpp ./third_party/

mkdir build
cd build

cmake -G "Ninja" \
   ${CMAKE_ARGS} \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CXX_STANDARD=17 \
    -DCMAKE_C_COMPILER=$C_COMPILER \
    -DCMAKE_CXX_COMPILER=$CXX_COMPILER \
    -DCMAKE_INSTALL_PREFIX=$LIBPROTO_INSTALL \
    -Dprotobuf_BUILD_TESTS=OFF \
    -Dprotobuf_BUILD_LIBUPB=OFF \
    -Dprotobuf_BUILD_SHARED_LIBS=ON \
    -Dprotobuf_ABSL_PROVIDER="module" \
    -Dprotobuf_JSONCPP_PROVIDER="package" \
    -Dprotobuf_USE_EXTERNAL_GTEST=OFF \
    ..
echo "building libprotobuf...."
cmake --build . --verbose
echo "Installing libprotobuf...."
cmake --install .

cd ..

#Build protobuf
export PROTOC=$LIBPROTO_DIR/build/protoc
export LD_LIBRARY_PATH=$CURRENT_DIR/abseil-cpp/abseilcpp/lib:$(pwd)/build/libprotobuf.so:$LD_LIBRARY_PATH
export LIBRARY_PATH=$(pwd)/build/libprotobuf.so:$LD_LIBRARY_PATH
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=cpp
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION_VERSION=2

#Apply patch
wget https://raw.githubusercontent.com/ppc64le/build-scripts/refs/heads/master/p/protobuf/set_cpp_to_17_v4.25.3.patch
git apply set_cpp_to_17_v4.25.3.patch

echo "Installing protobuf...."
cd python
python3 -m pip install . --no-build-isolation
cd $CURRENT_DIR

echo "------------ libprotobuf,protobuf installed--------------"

# Install PyTorch from the devpi index instead of building from source.
# This avoids the multi-hour source build (clone, patch, compile) and relies on
# the pre-built ppc64le wheel available at the internal devpi mirror.
echo "----------Installing pytorch from devpi index------------"
python3 -m pip install torch==${PYTORCH_VERSION#v} \
    --extra-index-url=https://wheels.developerfirst.ibm.com/ppc64le/linux/ \
    --prefer-binary

echo "--------------------------------- Installing Opus ---------------------------------"

git clone https://github.com/xiph/opus
cd opus
git checkout v1.3.1

mkdir opus_prefix
export OPUS_PREFIX=$(pwd)/opus_prefix

if [[ $(uname) == MSYS* ]]; then
  if [[ ${ARCH} == 32 ]]; then
    HOST_BUILD="--host=i686-w64-mingw32 --build=i686-w64-mingw32"
  else
    HOST_BUILD="--host=x86_64-w64-mingw32 --build=x86_64-w64-mingw32"
  fi
  OPUS_PREFIX=${OPUS_PREFIX}/Library/mingw-w64
  JOBS=${NUMBER_OF_PROCESSORS}
elif [[ $(uname) == Darwin ]]; then
  JOBS=$(sysctl -n hw.ncpu)
else
  JOBS=$(grep -c ^processor /proc/cpuinfo 2>/dev/null || 1)
fi

./autogen.sh
./configure --prefix=$OPUS_PREFIX $HOST_BUILD && make -j$JOBS && make install

#test
make check

echo "--------------------------------- Opus Installed Successfully ---------------------------------"

cd $CURRENT_DIR

# Clone the libvpx package
echo "--------------------------------- Installing libvpx ---------------------------------"

git clone https://github.com/webmproject/libvpx
cd libvpx
git checkout v1.13.1

mkdir libvpx_prefix
export LIBVPX_PREFIX=$(pwd)/libvpx_prefix

export target_platform=$(uname)-$(uname -m)
export GCC_HOME=/opt/rh/gcc-toolset-13/root/usr
export CC=$(which gcc)
export CXX=$(which g++)

# Get an updated config.sub and config.guess
#cp $BUILD_PREFIX/share/libtool/build-aux/config.* .


if [[ ${target_platform} == Linux-* ]]; then
    LDFLAGS="$LDFLAGS -pthread"
fi

CPU_DETECT="${CPU_DETECT} --enable-runtime-cpu-detect"

./configure --prefix=$LIBVPX_PREFIX $HOST_BUILD \
            --as=yasm \
            --enable-shared \
            --disable-static \
            --disable-install-docs \
            --disable-install-srcs \
            --enable-vp8 \
            --enable-postproc \
            --enable-vp9 \
            --enable-vp9-highbitdepth \
            --enable-pic \
            ${CPU_DETECT} \
            --enable-experimental || { cat config.log; exit 1; }
make -j${CPU_COUNT}
make install

echo "--------------------------------- libvpx Installed Successfully ---------------------------------"

cd $CURRENT_DIR

# Clone the libvpx package
echo "--------------------------------- Installing lame ---------------------------------"

wget https://downloads.sourceforge.net/sourceforge/lame/lame-3.100.tar.gz
tar -xvf lame-3.100.tar.gz
cd lame-3.100

mkdir lame_prefix
export LAME_PREFIX=$(pwd)/lame_prefix

export CPU_COUNT=$(nproc)

# Get an updated config.sub and config.guess
#cp -r ${BUILD_PREFIX}/share/libtool/build-aux/config.* .

# remove libtool files
find $LAME_PREFIX -name '*.la' -delete

./configure --prefix=$LAME_PREFIX \
            --disable-dependency-tracking \
            --disable-debug \
            --enable-shared \
            --enable-static \
            --enable-nasm

make -j$CPU_COUNT
make install

echo "--------------------------------- lame Installed Successfully ---------------------------------"

cd $CURRENT_DIR

#setting opus-prefix path on .pc file
cd opus/opus_prefix/
OPUS_PREFIX=$(pwd)
sed -i "/^prefix=/c\prefix=${OPUS_PREFIX}" $OPUS_PREFIX/lib/pkgconfig/opus.pc

cd $CURRENT_DIR

echo "---------------------------Installing FFmpeg------------------"
#Cloning Source Code
FFMPEG_PACKAGE_VERSION=${3:-n7.1}

git clone https://github.com/FFmpeg/FFmpeg
cd FFmpeg
git checkout $FFMPEG_PACKAGE_VERSION
git submodule update --init

mkdir ffmpeg_prefix

export FFMPEG_PREFIX=$(pwd)/ffmpeg_prefix

unset SUBDIR

export CPU_COUNT=$(nproc)

export CC=`which gcc`

export PKG_CONFIG_PATH=$CURRENT_DIR/opus/opus_prefix/lib/pkgconfig

USE_NONFREE=no   #the options below are set for NO

./configure \
        --prefix="$FFMPEG_PREFIX" \
        --cc=${CC} \
        --disable-doc \
        --enable-gmp \
        --enable-hardcoded-tables \
        --enable-libfreetype \
        --enable-pthreads \
        --enable-postproc \
        --enable-pic \
        --enable-pthreads \
        --enable-shared \
        --enable-static \
        --enable-version3 \
        --enable-zlib \
        --enable-libopus \
        --enable-libmp3lame \
        --enable-libvpx \
        --extra-cflags="-I$LAME_PREFIX/include -I$OPUS_PREFIX/include -I$LIBVPX_PREFIX/include" \
        --extra-ldflags="-L$LAME_PREFIX/lib -L$OPUS_PREFIX/lib -L$LIBVPX_PREFIX/lib" \
        --disable-encoder=h264 \
        --disable-decoder=h264 \
        --disable-decoder=libh264 \
        --disable-decoder=libx264 \
        --disable-decoder=libopenh264 \
        --disable-encoder=libopenh264 \
        --disable-encoder=libx264 \
        --disable-decoder=libx264rgb \
        --disable-encoder=libx264rgb \
        --disable-encoder=hevc \
        --disable-decoder=hevc \
        --disable-encoder=aac \
        --disable-decoder=aac \
        --disable-decoder=aac_fixed \
        --disable-encoder=aac_latm \
        --disable-decoder=aac_latm \
        --disable-encoder=mpeg \
        --disable-encoder=mpeg1video \
        --disable-encoder=mpeg2video \
        --disable-encoder=mpeg4 \
        --disable-encoder=msmpeg4 \
        --disable-encoder=mpeg4_v4l2m2m \
        --disable-encoder=msmpeg4v2 \
        --disable-encoder=msmpeg4v3 \
        --disable-decoder=mpeg \
        --disable-decoder=mpegvideo \
        --disable-decoder=mpeg1video \
        --disable-decoder=mpeg1_v4l2m2m \
        --disable-decoder=mpeg2video \
        --disable-decoder=mpeg2_v4l2m2m \
        --disable-decoder=mpeg4 \
        --disable-decoder=msmpeg4 \
        --disable-decoder=mpeg4_v4l2m2m \
        --disable-decoder=msmpeg4v1 \
        --disable-decoder=msmpeg4v2 \
        --disable-decoder=msmpeg4v3 \
        --disable-encoder=h264_v4l2m2m \
        --disable-decoder=h264_v4l2m2m \
        --disable-encoder=hevc_v4l2m2m \
        --disable-decoder=hevc_v4l2m2m \
        --disable-nonfree --disable-gpl --disable-gnutls --enable-openssl --disable-libopenh264 --disable-libx264    #"${_CONFIG_OPTS[@]}"

make -j$CPU_COUNT
make install -j$CPU_COUNT

echo "--------------------------------- ffmpeg Installed successfully ---------------------------------"

cd $CURRENT_DIR
mkdir -p local/ffmpeg
cp -r FFmpeg/ffmpeg_prefix/* local/ffmpeg/

FFMPEG_PACKAGE_VERSION=$(echo "$FFMPEG_PACKAGE_VERSION" | sed 's/[^0-9.]//g')

export LD_LIBRARY_PATH=${LAME_PREFIX}/lib:${LIBVPX_PREFIX}/lib:${OPUS_PREFIX}/lib:${FFMPEG_PREFIX}/lib:${LD_LIBRARY_PATH}

echo " ------------------------------------------ Checking Test ------------------------------------------ "

cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg --help
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -loglevel panic -protocols | grep "https"
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -loglevel panic -codecs | grep "libmp3lame"
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -loglevel panic -codecs | grep "DEVI.S zlib"
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -loglevel panic -codecs

cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -encoders
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -decoders
cd ${FFMPEG_PREFIX}/bin/ && ./ffmpeg -codecs >$CURRENT_DIR/ffmpeg-codecs.txt

if grep '\(h264\|aac\|hevc\|mpeg4\).*coders:' $HOME/ffmpeg-codecs.txt ; then
  echo >&2 -e "\nError: Forbidden codecs in ffmpeg, see lines above.\n"
  problem=true
else
  echo -e "OK, ffmpeg has no forbidden codecs."
fi

ffmpeg_libs="avcodec
        avdevice
        swresample
        avfilter
        avcodec
        avformat
        swscale"
for each_ffmpeg_lib in $ffmpeg_libs; do
  test -f $FFMPEG_PREFIX/lib/lib$each_ffmpeg_lib.so
done


export PKG_CONFIG_PATH=${FFMPEG_PREFIX}/lib/pkgconfig:${PKG_CONFIG_PATH}
export LD_LIBRARY_PATH=${FFMPEG_PREFIX}/lib:${LD_LIBRARY_PATH}
export PATH="/install-deps/ffmpeg/bin:$PATH"

cd $CURRENT_DIR

echo "--------------------------Installing pillow-----------------------------"
# Install Pillow from the devpi index instead of building from source.
python3 -m pip install Pillow==13.0.0 \
    --extra-index-url=https://wheels.developerfirst.ibm.com/ppc64le/linux/ \
    --prefer-binary

echo "--------------------Installing pyav----------------------------"
# PyAV's setup.py imports Cython directly at build time — install it first.
python3 -m pip install Cython
git clone https://github.com/PyAV-Org/PyAV
cd PyAV

# This command prints both versions, one per line, then sorts them using version-aware sort (-V)
# The smallest version will appear first
# If the smallest version is not 0.22.0, then VERSION must be less than 0.22.0

if [ "$(printf '%s\n' "$VERSION" "0.22.0" | sort -V | head -n1)" != "0.22.0" ]; then
    # VERSION is less than 0.22.0
    git checkout v13.1.0
else
    # VERSION is greater than or equal to 0.22.0
    # This PyAV version must match the runtime PyAV version.
    git checkout v14.4.0
    sed -i 's/license = "BSD-3-Clause"/license = {text = "BSD-3-Clause"}/' pyproject.toml
fi

git submodule update --init

export CFLAGS="${CFLAGS} -I/install-deps/ffmpeg/include"
export LDFLAGS="${LDFLAGS} -L/install-deps/ffmpeg/lib"

python3 setup.py build_ext --inplace
cd $CURRENT_DIR

echo "------------------Building torchvision------------------------"
git clone $PACKAGE_URL
cd $PACKAGE_NAME
git checkout $PACKAGE_VERSION

# Below patch is needed to exclude the models that come under SWAG license (CC-BY-NC-4.0)
# Using patch file v0.25.0 for PACKAGE_VERSION >= v0.25.0 (eg: v0.26.0).
wget https://raw.githubusercontent.com/ppc64le/build-scripts/refs/heads/master/t/torchvision/0001-Exclude-source-that-has-commercial-license_v0.25.0.patch
git apply 0001-Exclude-source-that-has-commercial-license_v0.25.0.patch


sed -i '/elif sha != "Unknown":/,+1d' setup.py

if ! python3 setup.py build; then
    echo "------------------$PACKAGE_NAME:install_fails-------------------------------------"
    echo "$PACKAGE_URL $PACKAGE_NAME"
    echo "$PACKAGE_NAME  |  $PACKAGE_URL | $PACKAGE_VERSION | $OS_NAME | GitHub | Fail |  Install_Fails"
    exit 1
fi

cd build
# PyTorch >= 2.14 is built as a PEP 517 wheel, so TorchConfig.cmake exists only in the installed package.
export CMAKE_PREFIX_PATH=$PYTHON_SITE_PACKAGES/torch/share/cmake/Torch:$CURRENT_DIR/pytorch/torch/share/cmake/Torch:$LIBPROTO_INSTALL
cmake ..
make install
cp libtorchvision.so $CURRENT_DIR/vision/torchvision/libtorchvision.so
cp libtorchvision.so $PYTHON_SITE_PACKAGES/torch/share/cmake/Torch
cp libtorchvision.so /usr/local/lib64
cd $CURRENT_DIR/vision

export LD_LIBRARY_PATH="${CURRENT_DIR}/pytorch/build/lib/:$LD_LIBRARY_PATH"
export LD_LIBRARY_PATH="${CURRENT_DIR}/protobuf/local/libprotobuf/lib64/:$LD_LIBRARY_PATH"

# Set TORCHVISION_BUILD_VERSION to strip the default 'a0' pre-release suffix that
# setup.py appends when the env var is unset, so the built wheel version matches
# any downstream requirement pinning torchvision==0.29.0 exactly.
TORCHVISION_BUILD_VERSION=${PACKAGE_VERSION#v} python3 setup.py bdist_wheel --dist-dir $CURRENT_DIR

cd $CURRENT_DIR

python3 -m pip install ./torchvision*.whl

python3 -m pip install pytest pytest-xdist

# Run tests
if ! pytest $PACKAGE_NAME/test/common_extended_utils.py $PACKAGE_NAME/test/common_utils.py $PACKAGE_NAME/test/smoke_test.py $PACKAGE_NAME/test/test_architecture_ops.py ; then
    echo "------------------$PACKAGE_NAME:install_success_but_test_fails---------------------"
    echo "$PACKAGE_URL $PACKAGE_NAME"
    echo "$PACKAGE_NAME  |  $PACKAGE_URL | $PACKAGE_VERSION | $OS_NAME | GitHub | Fail |  Install_success_but_test_Fails"
    exit 2
else
    echo "------------------$PACKAGE_NAME:install_&_test_both_success-------------------------"
    echo "$PACKAGE_URL $PACKAGE_NAME"
    echo "$PACKAGE_NAME  |  $PACKAGE_URL | $PACKAGE_VERSION | $OS_NAME | GitHub  | Pass |  Both_Install_and_Test_Success"
    exit 0
fi