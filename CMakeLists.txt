cmake_minimum_required(VERSION 3.16)
project(sing-canokey C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

option(ENABLE_DEBUG_OUTPUT "Print canokey-core debug messages" OFF)

set(VIRTCARD ON CACHE BOOL "" FORCE)
set(ENABLE_NFC 1 CACHE STRING "" FORCE)
set(ENABLE_APPLET_NDEF 1 CACHE STRING "" FORCE)
set(ENABLE_PASS 1 CACHE STRING "" FORCE)
set(ENABLE_IFACE_CTAPHID 1 CACHE STRING "" FORCE)
set(ENABLE_IFACE_WEBUSB 1 CACHE STRING "" FORCE)
set(ENABLE_IFACE_CCID 1 CACHE STRING "" FORCE)
set(ENABLE_IFACE_KBDHID 1 CACHE STRING "" FORCE)
set(PRODUCT_NAME "CanoKey USB/IP" CACHE STRING "" FORCE)

add_subdirectory(canokey-core EXCLUDE_FROM_ALL)

# canokey-crypto's configure step re-applies its Ed25519 patch to tf-psa-crypto in-source with
# `patch -N` on every run; on the already-patched tree this leaves *.orig/*.rej rejects behind.
file(GLOB_RECURSE TF_PSA_CRYPTO_PATCH_REJECTS
    canokey-core/canokey-crypto/tf-psa-crypto/*.orig
    canokey-core/canokey-crypto/tf-psa-crypto/*.rej)
file(REMOVE ${TF_PSA_CRYPTO_PATCH_REJECTS})

# GNU ld cannot resolve GCC's .weak.* pseudo-symbol scheme for PE weak externals across
# static-archive members. canokey-crypto's weak definitions are never overridden in the
# virt build, so compile them as strong symbols on Windows.
if(WIN32)
    target_compile_definitions(canokey-crypto PRIVATE weak=)
endif()

add_library(canokey-virt STATIC
    canokey-core/virt-card/device-sim.c
    canokey-core/virt-card/fabrication.c
    canokey-core/littlefs/bd/lfs_filebd.c
)
target_include_directories(canokey-virt SYSTEM PRIVATE
    canokey-core/virt-card
    canokey-core
)
target_compile_definitions(canokey-virt PRIVATE HW_VARIANT_NAME="CanoKey USB/IP")
target_link_libraries(canokey-virt canokey-core)
add_dependencies(canokey-virt gitrev)

add_custom_target(canokey ALL DEPENDS canokey-virt canokey-core)
