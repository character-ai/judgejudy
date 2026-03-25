#!/usr/bin/env python3
"""Audio metrics: pesq, stoi, utmos."""

import argparse
import json
import sys


def _load_audio(path: str, target_sr: int):
    """Load audio file and resample to target sample rate. Supports WAV, MP3, etc."""
    import numpy as np

    # Try torchaudio first — handles MP3, WAV, FLAC, OGG, etc.
    try:
        import torchaudio

        waveform, sr = torchaudio.load(path)
        # Convert to mono
        if waveform.shape[0] > 1:
            waveform = waveform.mean(dim=0, keepdim=True)
        # Resample if needed
        if sr != target_sr:
            waveform = torchaudio.functional.resample(waveform, sr, target_sr)
        return waveform.squeeze(0).numpy(), target_sr
    except ImportError:
        pass

    # Fallback: soundfile (WAV/FLAC only)
    try:
        import soundfile as sf

        audio, sr = sf.read(path, dtype="float32")
        if len(audio.shape) > 1:
            audio = audio[:, 0]
        if sr != target_sr:
            ratio = target_sr / sr
            n_samples = int(len(audio) * ratio)
            indices = np.linspace(0, len(audio) - 1, n_samples).astype(int)
            audio = audio[indices]
        return audio, target_sr
    except ImportError:
        pass

    # Fallback: scipy (WAV only)
    try:
        from scipy.io import wavfile

        sr, audio = wavfile.read(path)
        if audio.dtype == np.int16:
            audio = audio.astype(np.float32) / 32768.0
        elif audio.dtype == np.int32:
            audio = audio.astype(np.float32) / 2147483648.0
        elif audio.dtype != np.float32:
            audio = audio.astype(np.float32)
        if len(audio.shape) > 1:
            audio = audio[:, 0]
        if sr != target_sr:
            ratio = target_sr / sr
            n_samples = int(len(audio) * ratio)
            indices = np.linspace(0, len(audio) - 1, n_samples).astype(int)
            audio = audio[indices]
        return audio, target_sr
    except ImportError:
        raise ImportError(
            "No audio loading library found. Run: pip install torchaudio or pip install soundfile"
        )


def compute_pesq(data: dict) -> dict:
    try:
        from pesq import pesq as pesq_fn
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "pesq package not installed. Run: pip install pesq",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    reference_path = data.get("reference_path") or data.get("reference_file_path")
    if not generated_path or not reference_path:
        return {
            "score": None,
            "raw": None,
            "error": "pesq metric requires 'generated_path' and 'reference_path' fields",
        }

    params = data.get("params", {})
    sample_rate = params.get("sample_rate", 16000)
    mode = "wb" if sample_rate == 16000 else "nb"

    try:
        ref_audio, _ = _load_audio(reference_path, sample_rate)
        gen_audio, _ = _load_audio(generated_path, sample_rate)
    except ImportError as e:
        return {"score": None, "raw": None, "error": str(e)}

    # Trim to same length
    min_len = min(len(ref_audio), len(gen_audio))
    ref_audio = ref_audio[:min_len]
    gen_audio = gen_audio[:min_len]

    pesq_value = pesq_fn(sample_rate, ref_audio, gen_audio, mode)

    # Normalize from [-0.5, 4.5] to [0, 1]
    normalized = (pesq_value - (-0.5)) / (4.5 - (-0.5))
    normalized = max(0.0, min(1.0, normalized))

    return {
        "score": normalized,
        "raw": {"pesq": pesq_value, "normalized": normalized, "mode": mode},
        "error": None,
    }


def compute_stoi(data: dict) -> dict:
    try:
        from pystoi import stoi as stoi_fn
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "pystoi package not installed. Run: pip install pystoi",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    reference_path = data.get("reference_path") or data.get("reference_file_path")
    if not generated_path or not reference_path:
        return {
            "score": None,
            "raw": None,
            "error": "stoi metric requires 'generated_path' and 'reference_path' fields",
        }

    params = data.get("params", {})
    sample_rate = params.get("sample_rate", 16000)
    extended = params.get("extended", False)

    try:
        ref_audio, _ = _load_audio(reference_path, sample_rate)
        gen_audio, _ = _load_audio(generated_path, sample_rate)
    except ImportError as e:
        return {"score": None, "raw": None, "error": str(e)}

    min_len = min(len(ref_audio), len(gen_audio))
    ref_audio = ref_audio[:min_len]
    gen_audio = gen_audio[:min_len]

    stoi_value = stoi_fn(ref_audio, gen_audio, sample_rate, extended=extended)

    return {
        "score": stoi_value,
        "raw": {"stoi": stoi_value, "extended": extended},
        "error": None,
    }


def compute_utmos(data: dict) -> dict:
    try:
        import torch
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "torch package not installed. Run: pip install torch",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    if not generated_path:
        return {
            "score": None,
            "raw": None,
            "error": "utmos metric requires 'generated_path' field",
        }

    params = data.get("params", {})
    sample_rate = params.get("sample_rate", 16000)

    try:
        predictor = torch.hub.load(
            "tarepan/SpeechMOS:v1.2.0", "utmos22_strong", trust_repo=True
        )
    except Exception as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Failed to load UTMOS model: {e}. Ensure torch and torchaudio are installed.",
        }

    try:
        import torchaudio

        waveform, sr = torchaudio.load(generated_path)
        if waveform.shape[0] > 1:
            waveform = waveform.mean(dim=0, keepdim=True)
        if sr != sample_rate:
            waveform = torchaudio.functional.resample(waveform, sr, sample_rate)
        mos = predictor(waveform, sample_rate).item()
    except Exception as e:
        return {
            "score": None,
            "raw": None,
            "error": f"UTMOS prediction failed: {e}",
        }

    # MOS typically in [1, 5], normalize to [0, 1]
    normalized = (mos - 1.0) / 4.0
    normalized = max(0.0, min(1.0, normalized))

    return {
        "score": normalized,
        "raw": {"mos": mos, "normalized": normalized},
        "error": None,
    }


METRICS = {
    "pesq": compute_pesq,
    "stoi": compute_stoi,
    "utmos": compute_utmos,
}


def main():
    parser = argparse.ArgumentParser(description="Compute audio metrics")
    parser.add_argument("--input", required=True, help="Path to input JSON file")
    parser.add_argument("--output", required=True, help="Path to output JSON file")
    args = parser.parse_args()

    try:
        with open(args.input, "r") as f:
            data = json.load(f)

        metric = data.get("metric")
        if metric not in METRICS:
            result = {
                "score": None,
                "raw": None,
                "error": f"Unknown metric: {metric}. Supported: {list(METRICS.keys())}",
            }
        else:
            result = METRICS[metric](data)

        with open(args.output, "w") as f:
            json.dump(result, f, indent=2)

        if result.get("error"):
            sys.exit(1)
        sys.exit(0)

    except Exception as e:
        result = {"score": None, "raw": None, "error": str(e)}
        try:
            with open(args.output, "w") as f:
                json.dump(result, f, indent=2)
        except Exception:
            print(json.dumps(result), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
