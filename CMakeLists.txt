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
