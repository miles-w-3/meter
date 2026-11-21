#include "parakeet_capi.h"
#include <array>
#include <cstdint>
#include <cstdio>
#include <iostream>
#include <memory>
#include <string>
#include <vector>

static void send(char kind, const std::string& text = {}) {
    const auto size = static_cast<uint32_t>(text.size());
    const std::array<char, 5> header{kind, char(size >> 24), char(size >> 16), char(size >> 8), char(size)};
    std::cout.write(header.data(), header.size()).write(text.data(), text.size()).flush();
}

int main(int argc, char** argv) {
    if (argc != 2) return 2;
    std::ios::sync_with_stdio(false);
    std::cin.tie(nullptr);

    using Model = std::unique_ptr<parakeet_ctx, decltype(&parakeet_capi_free)>;
    using Stream = std::unique_ptr<parakeet_stream, decltype(&parakeet_capi_stream_free)>;
    using Text = std::unique_ptr<char, decltype(&parakeet_capi_free_string)>;
    Model model(parakeet_capi_load(argv[1]), parakeet_capi_free);
    if (!model) {
        std::cerr << parakeet_capi_load_error() << '\n';
        return 1;
    }

    send('R');
    Stream stream(nullptr, parakeet_capi_stream_free);
    std::string transcript;
    std::array<unsigned char, 5> header{};
    while (std::cin.read(reinterpret_cast<char*>(header.data()), header.size())) {
        const auto size = (uint32_t(header[1]) << 24) | (uint32_t(header[2]) << 16) |
                          (uint32_t(header[3]) << 8) | uint32_t(header[4]);
        if (size > 4096 || size % 2 || (header[0] != 'A' && size != 0)) return 2;
        std::vector<unsigned char> bytes(size);
        if (size && !std::cin.read(reinterpret_cast<char*>(bytes.data()), size)) return 1;
        switch (header[0]) {
        case 'S':
            stream.reset(parakeet_capi_stream_begin(model.get()));
            if (!stream) {
                std::cerr << parakeet_capi_last_error(model.get()) << '\n';
                return 1;
            }
            transcript.clear();
            send('S');
            break;
        case 'A': {
            if (!stream || size == 0) return 2;
            std::vector<float> pcm(size / 2);
            for (size_t i = 0; i < pcm.size(); ++i) {
                const int word = int(bytes[2 * i]) | (int(bytes[2 * i + 1]) << 8);
                pcm[i] = float(word < 32768 ? word : word - 65536) / 32768.0f;
            }
            Text delta(parakeet_capi_stream_feed(stream.get(), pcm.data(), int(pcm.size()), nullptr),
                       parakeet_capi_free_string);
            if (!delta) return 1;
            if (*delta) {
                transcript += delta.get();
                send('P', transcript);
            }
            break;
        }
        case 'F': {
            if (!stream) return 2;
            Text delta(parakeet_capi_stream_finalize(stream.get()), parakeet_capi_free_string);
            if (!delta) return 1;
            transcript += delta.get();
            send('F', transcript);
            stream.reset();
            break;
        }
        default:
            return 2;
        }
    }
    return std::cin.eof() ? 0 : 1;
}
