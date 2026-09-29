#!/usr/bin/env python3
"""Offline Helm lint/render contract checks; requires helm and PyYAML."""
import copy
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
CHART = ROOT / "deploy/helm/sub2api-public-transit"
EXAMPLES = ROOT / "deploy/helm/examples"
BASE = yaml.safe_load((EXAMPLES / "values-minimal.yaml").read_text())


class UniqueKeyLoader(yaml.SafeLoader):
    """Reject duplicate mapping keys instead of silently accepting bad manifests."""


def unique_mapping(loader, node):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if key in result:
            raise ValueError(f"Duplicate YAML key: {key}")
        result[key] = loader.construct_object(value_node)
    return result


UniqueKeyLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping
)


def merge(left, right):
    result = copy.deepcopy(left)
    for key, value in right.items():
        result[key] = (
            merge(result[key], value)
            if isinstance(value, dict) and isinstance(result.get(key), dict)
            else copy.deepcopy(value)
        )
    return result


class ChartTest(unittest.TestCase):
    def helm(self, args, expected=0):
        result = subprocess.run(
            ["helm", *map(str, args)], text=True, capture_output=True, check=False
        )
        if expected == 0:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout + result.stderr if expected else result.stdout

    def render(self, patch=None, files=None):
        with tempfile.TemporaryDirectory(prefix="transit-helm-") as tmp:
            values = Path(tmp) / "values.yaml"
            values.write_text(yaml.safe_dump(merge(BASE, patch or {})))
            flags = ["-f", values]
            for path in files or []:
                flags.extend(["-f", path])
            self.helm(["lint", "--strict", CHART, *flags])
            rendered = self.helm([
                "template", "transit", CHART, "--namespace", "transit-test", *flags
            ])
        docs = [d for d in yaml.load_all(rendered, Loader=UniqueKeyLoader) if d]
        self.assertEqual(len({(d["kind"], d["metadata"]["name"]) for d in docs}), len(docs))
        self.assertTrue(all(d["kind"] in {
            "Deployment", "Service", "ConfigMap", "PersistentVolumeClaim", "Ingress"
        } for d in docs))
        self.assertEqual(sum(d["kind"] == "Deployment" for d in docs), 1)
        return {d["kind"]: d for d in docs}

    @staticmethod
    def pod(docs):
        return docs["Deployment"]["spec"]["template"]["spec"]

    def test_baseline_contract(self):
        docs = self.render()
        self.assertEqual(set(docs), {"Deployment", "Service", "ConfigMap", "PersistentVolumeClaim"})
        dep = docs["Deployment"]
        pod = self.pod(docs)
        self.assertEqual(dep["spec"]["replicas"], 1)
        self.assertEqual(dep["spec"]["progressDeadlineSeconds"], 900)
        self.assertEqual(dep["spec"]["strategy"], {"type": "Recreate"})
        self.assertEqual(dep["spec"]["selector"]["matchLabels"], dep["spec"]["template"]["metadata"]["labels"])
        self.assertEqual(docs["Service"]["spec"]["selector"], dep["spec"]["selector"]["matchLabels"])
        self.assertFalse(pod["automountServiceAccountToken"])
        for key in ("runAsUser", "runAsGroup", "fsGroup"):
            self.assertEqual(pod["securityContext"][key], 1000)
        self.assertTrue(pod["securityContext"]["runAsNonRoot"])
        self.assertEqual(pod["nodeSelector"], {"kubernetes.io/os": "linux", "kubernetes.io/arch": "amd64"})
        self.assertEqual(len(pod["containers"]), 1)
        self.assertNotIn("initContainers", pod)
        container = pod["containers"][0]
        self.assertFalse(container["securityContext"]["allowPrivilegeEscalation"])
        self.assertEqual(container["securityContext"]["capabilities"]["drop"], ["ALL"])
        self.assertEqual(container["ports"][0]["containerPort"], 8080)
        self.assertEqual(container["image"], "ghcr.io/superpx-cn/sub2api-public-transit:REPLACE_WITH_PUBLISHED_TAG")
        self.assertEqual(container["volumeMounts"], [{"name": "data", "mountPath": "/app/data"}])
        self.assertEqual(pod["volumes"], [{"name": "data", "persistentVolumeClaim": {"claimName": "transit"}}])
        self.assertEqual(container["envFrom"], [{"configMapRef": {"name": "transit"}}])
        env = {e["name"]: e for e in container["env"]}
        self.assertEqual(len(env), len(container["env"]))
        for key in ("DATABASE_PASSWORD", "JWT_SECRET", "TOTP_ENCRYPTION_KEY", "ADMIN_PASSWORD"):
            self.assertEqual(env[key], {"name": key, "valueFrom": {"secretKeyRef": {"name": "transit-secrets", "key": key}}})
        self.assertEqual(env["REDIS_PASSWORD"], {"name": "REDIS_PASSWORD", "value": ""})
        config = docs["ConfigMap"]["data"]
        self.assertTrue(all(isinstance(v, str) for v in config.values()))
        for key in ("DATABASE_PASSWORD", "JWT_SECRET", "TOTP_ENCRYPTION_KEY", "ADMIN_PASSWORD", "REDIS_PASSWORD"):
            self.assertNotIn(key, config)
        for key, value in {"AUTO_SETUP": "true", "SERVER_PORT": "8080", "SERVER_HOST": "0.0.0.0", "DATA_DIR": "/app/data", "REDIS_ENABLE_TLS": "false"}.items():
            self.assertEqual(config[key], value)
        for name in ("startup", "readiness", "liveness"):
            self.assertEqual(container[name + "Probe"]["httpGet"], {"path": "/health", "port": "http"})
        startup = container["startupProbe"]
        self.assertEqual(startup["periodSeconds"] * startup["failureThreshold"], 600)
        pvc = docs["PersistentVolumeClaim"]
        self.assertEqual(pvc["metadata"]["annotations"]["helm.sh/resource-policy"], "keep")
        self.assertEqual(pvc["spec"]["accessModes"], ["ReadWriteOnce"])
        self.assertEqual(pvc["spec"]["resources"]["requests"]["storage"], "1Gi")
        self.assertNotIn("storageClassName", pvc["spec"])
        service = docs["Service"]["spec"]
        self.assertEqual(service["type"], "ClusterIP")
        self.assertEqual(service["ports"][0]["targetPort"], "http")
        self.assertEqual(service["ports"][0]["port"], 8080)
        self.assertFalse(yaml.safe_load((CHART / "Chart.yaml").read_text()).get("dependencies"))

    def test_documented_examples(self):
        documented = (ROOT / "deploy/HELM_CN.md").read_text()
        for file in sorted(EXAMPLES.glob("values-*.yaml")):
            with self.subTest(example=file.name):
                self.assertIn(file.name, documented)
                docs = self.render(files=[file])
                if file.name == "values-ingress-tls.yaml":
                    ingress = docs["Ingress"]
                    self.assertEqual(ingress["spec"]["ingressClassName"], "nginx")
                    self.assertEqual(ingress["spec"]["tls"][0]["secretName"], "transit-tls")
                    rule = ingress["spec"]["rules"][0]
                    self.assertEqual(rule["host"], "transit.example.com")
                    self.assertEqual(rule["http"]["paths"][0]["backend"]["service"], {"name": "transit", "port": {"number": 8080}})
                    self.assertEqual(ingress["metadata"]["annotations"]["nginx.ingress.kubernetes.io/proxy-buffering"], "off")
                if file.name == "values-external-tls.yaml":
                    config = docs["ConfigMap"]["data"]
                    self.assertEqual(config["DATABASE_SSLMODE"], "verify-full")
                    self.assertEqual(config["REDIS_ENABLE_TLS"], "true")
                    self.assertEqual(config["REDIS_USERNAME"], "default")
                    self.assertEqual(config["REDIS_PORT"], "6380")
                    pod = self.pod(docs)
                    container = pod["containers"][0]
                    env = {e["name"]: e for e in container["env"]}
                    self.assertEqual(env["REDIS_PASSWORD"]["valueFrom"]["secretKeyRef"], {"name": "transit-secrets", "key": "REDIS_PASSWORD"})
                    self.assertEqual(env["PGSSLROOTCERT"]["value"], "/app/certs/ca.crt")
                    self.assertEqual(env["SSL_CERT_FILE"]["value"], "/app/certs/ca.crt")
                    self.assertEqual(pod["volumes"][1]["secret"]["secretName"], "transit-external-ca")
                    self.assertTrue(container["volumeMounts"][1]["readOnly"])

    def test_inline_documented_values(self):
        document = (ROOT / "deploy/HELM_CN.md").read_text()
        examples = re.findall(r"```yaml\n(.*?)\n```", document, re.DOTALL)
        self.assertTrue(examples, "Expected at least one inline YAML example")
        for example in examples:
            docs = self.render(yaml.load(example, Loader=UniqueKeyLoader))
            self.assertEqual(docs["Deployment"]["spec"]["progressDeadlineSeconds"], 1500)

    def test_storage_variants(self):
        docs = self.render({"persistence": {"existingClaim": "restored-data"}})
        self.assertNotIn("PersistentVolumeClaim", docs)
        self.assertEqual(self.pod(docs)["volumes"][0]["persistentVolumeClaim"]["claimName"], "restored-data")
        for storage_class in ("fast-ssd", "-"):
            with self.subTest(storage_class=storage_class):
                pvc = self.render({"persistence": {"storageClass": storage_class, "size": "5Gi", "retain": False}})["PersistentVolumeClaim"]
                self.assertEqual(pvc["spec"]["storageClassName"], "" if storage_class == "-" else storage_class)
                self.assertEqual(pvc["spec"]["resources"]["requests"]["storage"], "5Gi")
                self.assertNotIn("annotations", pvc["metadata"])

    def test_image_digest_and_private_registry(self):
        for tag in ("", "ignored-tag"):
            with self.subTest(tag=tag):
                digest = "sha256:" + "a" * 64
                pod = self.pod(self.render({
                    "image": {"repository": "registry.example.com/team/transit", "tag": tag, "digest": digest},
                    "imagePullSecrets": [{"name": "registry-credentials"}],
                    "nodeSelector": {"kubernetes.io/arch": "arm64"},
                }))
                self.assertEqual(pod["containers"][0]["image"], "registry.example.com/team/transit@" + digest)
                self.assertEqual(pod["imagePullSecrets"], [{"name": "registry-credentials"}])
                self.assertEqual(pod["nodeSelector"]["kubernetes.io/arch"], "arm64")

    def test_custom_settings(self):
        patch = {
            "externalDatabase": {"port": 5433, "username": "custom", "database": "transit_db", "existingSecret": "database", "passwordKey": "password"},
            "externalRedis": {"database": 2, "auth": {"enabled": True, "existingSecret": "cache", "passwordKey": "token"}},
            "app": {"existingSecret": "application", "secretKeys": {"jwt": "jwt", "totp": "totp", "adminPassword": "admin"}},
            "resources": {"requests": {"cpu": "250m", "memory": "256Mi"}},
            "tolerations": [{"key": "dedicated", "operator": "Equal", "value": "transit", "effect": "NoSchedule"}],
            "affinity": {"nodeAffinity": {"preferredDuringSchedulingIgnoredDuringExecution": [{"weight": 1, "preference": {"matchExpressions": [{"key": "zone", "operator": "In", "values": ["a"]}]}}]}},
            "probes": {"startup": {"failureThreshold": 120}},
            "podAnnotations": {"example.com/team": "ops"},
            "extraEnv": [{"name": "SETUP_MIGRATION_TIMEOUT_SECONDS", "value": "900"}, {"name": "EXTRA_TOKEN", "valueFrom": {"secretKeyRef": {"name": "extra", "key": "token"}}}],
            "service": {"type": "LoadBalancer", "port": 80, "annotations": {"example.com/key": "value"}},
        }
        docs = self.render(patch)
        pod = self.pod(docs)
        container = pod["containers"][0]
        env = {e["name"]: e for e in container["env"]}
        for key, secret, field in [("DATABASE_PASSWORD", "database", "password"), ("REDIS_PASSWORD", "cache", "token"), ("JWT_SECRET", "application", "jwt"), ("TOTP_ENCRYPTION_KEY", "application", "totp"), ("ADMIN_PASSWORD", "application", "admin")]:
            self.assertEqual(env[key]["valueFrom"]["secretKeyRef"], {"name": secret, "key": field})
        self.assertEqual(env["SETUP_MIGRATION_TIMEOUT_SECONDS"]["value"], "900")
        self.assertEqual(env["EXTRA_TOKEN"]["valueFrom"]["secretKeyRef"]["name"], "extra")
        self.assertEqual(container["resources"], patch["resources"])
        self.assertEqual(pod["affinity"], patch["affinity"])
        self.assertEqual(pod["tolerations"], patch["tolerations"])
        self.assertEqual(container["startupProbe"]["failureThreshold"], 120)
        self.assertEqual(docs["Deployment"]["spec"]["progressDeadlineSeconds"], 1500)
        self.assertEqual(docs["ConfigMap"]["data"]["REDIS_DB"], "2")
        self.assertEqual(docs["ConfigMap"]["data"]["DATABASE_PORT"], "5433")
        self.assertEqual(docs["Service"]["spec"]["ports"][0]["port"], 80)
        self.assertEqual(docs["Service"]["spec"]["type"], "LoadBalancer")
        self.assertEqual(docs["Service"]["metadata"]["annotations"], patch["service"]["annotations"])

    def test_config_checksum_and_names(self):
        first = self.render({"fullnameOverride": "", "nameOverride": "gateway"})
        second = self.render({"fullnameOverride": "", "nameOverride": "gateway", "app": {"timezone": "UTC"}})
        for resource in first.values():
            self.assertEqual(resource["metadata"]["name"], "transit-gateway")
        def checksum(docs):
            return docs["Deployment"]["spec"]["template"]["metadata"]["annotations"]["checksum/config"]
        self.assertNotEqual(checksum(first), checksum(second))
        self.assertEqual(checksum(first), checksum(self.render({"fullnameOverride": "", "nameOverride": "gateway"})))

    def test_reject_invalid_configuration(self):
        cases = [
            ({"image": {"tag": ""}}, "image"),
            ({"image": {"digest": "sha256:invalid"}}, "digest"),
            ({"externalDatabase": {"host": ""}}, "host"),
            ({"externalRedis": {"host": ""}}, "host"),
            ({"externalDatabase": {"existingSecret": ""}}, "existingSecret"),
            ({"app": {"existingSecret": ""}}, "existingSecret"),
            ({"app": {"secretKeys": {"jwt": ""}}}, "jwt"),
            ({"app": {"secretKeys": {"totp": ""}}}, "totp"),
            ({"app": {"secretKeys": {"adminPassword": ""}}}, "adminPassword"),
            ({"externalDatabase": {"passwordKey": ""}}, "passwordKey"),
            ({"externalRedis": {"auth": {"enabled": True}}}, "externalRedis.auth.existingSecret"),
            ({"externalRedis": {"auth": {"enabled": True, "existingSecret": "cache", "passwordKey": ""}}}, "passwordKey"),
            ({"externalDatabase": {"database": "db;drop"}}, "database"),
            ({"externalDatabase": {"port": 0}}, "port"),
            ({"externalDatabase": {"sslmode": "prefer"}}, "sslmode"),
            ({"ingress": {"enabled": True}}, "ingress.hosts"),
            ({"ingress": {"enabled": True, "hosts": [{"host": "transit.example.com", "paths": []}]}}, "paths"),
            ({"extraEnv": [{"name": "SERVER_PORT", "value": "80"}]}, "managed by the chart"),
            ({"extraEnv": [{"name": "JWT_SECRET", "value": "plaintext"}]}, "managed by the chart"),
            ({"extraEnv": [{"name": "X", "value": "1"}, {"name": "X", "value": "2"}]}, "duplicated"),
            ({"extraEnv": [{"name": "X", "value": 1}]}, "extraEnv"),
            ({"extraEnv": [{"name": "X", "value": "x", "valueFrom": {"fieldRef": {"fieldPath": "metadata.name"}}}]}, "extraEnv"),
            ({"extraVolumes": [{"name": "data", "emptyDir": {}}]}, "reserved"),
            ({"extraVolumeMounts": [{"name": "missing", "mountPath": "/certs"}]}, "unknown volume"),
            ({"extraVolumes": [{"name": "cert", "emptyDir": {}}], "extraVolumeMounts": [{"name": "cert", "mountPath": "/app/data/config.yaml"}]}, "shadow /app/data"),
            ({"probes": {"startup": {"failureThreshold": 0}}}, "failureThreshold"),
            ({"podAnnotations": {"checksum/config": "fake"}}, "managed by the chart"),
            ({"replicaCount": 2}, "replicaCount"),
        ]
        for patch, message in cases:
            with self.subTest(patch=patch), tempfile.TemporaryDirectory() as tmp:
                values = Path(tmp) / "invalid.yaml"
                values.write_text(yaml.safe_dump(merge(BASE, patch)))
                output = self.helm(["template", "transit", CHART, "-f", values], expected=1)
                self.assertIn(message, output)
        self.helm(["lint", "--strict", CHART], expected=1)
        self.helm(["template", "transit", CHART], expected=1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
