add_executable(meter-dictation "${CMAKE_CURRENT_LIST_DIR}/worker.cpp")
target_compile_features(meter-dictation PRIVATE cxx_std_17)
target_link_libraries(meter-dictation PRIVATE parakeet)
