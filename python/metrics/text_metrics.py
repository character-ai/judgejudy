#!/usr/bin/env python3
"""Text metrics: bertscore, rouge, bleu."""

import argparse
import json
import sys


def compute_bertscore(generated: str, reference: str, params: dict) -> dict:
    try:
        from bert_score import score as bert_score_fn
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "bert-score package not installed. Run: pip install bert-score",
        }

    lang = params.get("lang", "en")
    model_type = params.get("model_type", None)

    kwargs = {"lang": lang, "verbose": False}
    if model_type:
        kwargs["model_type"] = model_type

    P, R, F1 = bert_score_fn([generated], [reference], **kwargs)
    precision = P.item()
    recall = R.item()
    f1 = F1.item()

    return {
        "score": f1,
        "raw": {"precision": precision, "recall": recall, "f1": f1},
        "error": None,
    }


def compute_rouge(generated: str, reference: str, params: dict) -> dict:
    try:
        from rouge_score import rouge_scorer
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "rouge-score package not installed. Run: pip install rouge-score",
        }

    rouge_types = params.get("rouge_types", ["rougeL"])
    scorer = rouge_scorer.RougeScorer(rouge_types, use_stemmer=True)
    scores = scorer.score(reference, generated)

    raw = {}
    for key, value in scores.items():
        raw[key] = {
            "precision": value.precision,
            "recall": value.recall,
            "fmeasure": value.fmeasure,
        }

    # Use ROUGE-L F1 as the primary score
    primary_key = "rougeL" if "rougeL" in scores else list(scores.keys())[0]
    primary_score = scores[primary_key].fmeasure

    return {
        "score": primary_score,
        "raw": raw,
        "error": None,
    }


def compute_bleu(generated: str, reference: str, params: dict) -> dict:
    try:
        from nltk.translate.bleu_score import sentence_bleu, SmoothingFunction
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "nltk package not installed. Run: pip install nltk",
        }

    smoothing = SmoothingFunction().method1
    weights = params.get("weights", (0.25, 0.25, 0.25, 0.25))
    if isinstance(weights, list):
        weights = tuple(weights)

    reference_tokens = reference.split()
    generated_tokens = generated.split()

    score = sentence_bleu(
        [reference_tokens],
        generated_tokens,
        weights=weights,
        smoothing_function=smoothing,
    )

    return {
        "score": score,
        "raw": {"bleu": score, "weights": list(weights)},
        "error": None,
    }


METRICS = {
    "bertscore": compute_bertscore,
    "rouge": compute_rouge,
    "bleu": compute_bleu,
}


def main():
    parser = argparse.ArgumentParser(description="Compute text metrics")
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
            generated = data.get("generated_output", data.get("generated", ""))
            reference = data.get("expected_output", data.get("reference", ""))
            params = data.get("params", {})
            result = METRICS[metric](generated, reference, params)

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
