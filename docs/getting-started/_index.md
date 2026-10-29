---
title: "Getting started"
linkTitle: "Getting started"
weight: 1
description: >
  What shoot-grafter is and what it does
---

## About this project

**shoot-grafter** is a Kubernetes operator that automates the onboarding of [Gardener](https://gardener.cloud/) Shoots (Kubernetes clusters managed by Gardener) to [Greenhouse](https://github.com/cloudoperators/greenhouse), the cloud operations platform. It bridges the gap between Gardener-managed infrastructure and Greenhouse's centralized cluster management by dynamically discovering and registering Shoots as Greenhouse Clusters.

The name "shoot-grafter" continues Gardener's and Greenhouse's botanical theme: just as a gardener grafts shoots onto rootstock to create robust plants, this operator grafts (connects) Gardener Shoots onto Greenhouse's centralized platform to create fully operational k8s clusters.

This project is part of the [NeoNephos Foundation](https://neonephos.org/).

## What does shoot-grafter do?

shoot-grafter continuously monitors Garden clusters for Shoots matching specific criteria and automatically:

1. **Discovers Shoots**: Watches for Gardener Shoot resources in specified namespaces based on label selectors and CEL expressions
2. **Extracts cluster credentials**: Retrieves API server URLs and CA certificates from Shoot resources
3. **Creates Greenhouse Clusters**: Automatically registers discovered Shoots as Greenhouse Cluster resources
4. **Propagates labels**: Transfers specified labels from Shoots to Greenhouse Clusters for consistent organization
5. **Configures OIDC authentication**: Optionally configures OIDC authentication on Shoot clusters to enable Greenhouse authentication
6. **Configures RBAC**: Optionally sets up role-based access control on Shoot clusters for Greenhouse service accounts
7. **Maintains synchronization**: Keeps Greenhouse Cluster resources in sync with their corresponding Shoots

shoot-grafter currently does not automatically clean up clusters when Shoot labels change. Manual cleanup of Greenhouse Cluster resources is required in such a scenario.
