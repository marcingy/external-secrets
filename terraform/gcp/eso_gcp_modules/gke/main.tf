resource "google_service_account" "default" {
  account_id = var.GCP_GSA_NAME
}

resource "google_project_iam_member" "secretadmin" {
  project = var.project_id
  role    = "roles/secretmanager.admin"
  member  = "serviceAccount:${google_service_account.default.email}"
}

resource "google_project_iam_member" "service_account_token_creator" {
  project = var.project_id
  role    = "roles/iam.serviceAccountTokenCreator"
  member  = "serviceAccount:${google_service_account.default.email}"
}

resource "google_service_account_iam_member" "pod_identity" {
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[default/${var.GCP_KSA_NAME}]"
  service_account_id = google_service_account.default.name
}

resource "google_service_account_iam_member" "pod_identity_e2e" {
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[default/external-secrets-e2e]"
  service_account_id = google_service_account.default.name
}

resource "google_container_cluster" "primary" {
  name                     = "${var.env}-cluster"
  location                 = var.zone
  remove_default_node_pool = true
  initial_node_count       = var.initial_node_count
  network                  = var.network
  subnetwork               = var.subnetwork
  ip_allocation_policy {}
  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }
  resource_labels = {
    "example" = "value"
  }
}

resource "google_container_node_pool" "nodes" {
  name       = "${google_container_cluster.primary.name}-node-pool"
  location   = google_container_cluster.primary.location
  cluster    = google_container_cluster.primary.name
  node_count = var.node_count

  node_config {
    preemptible     = var.preemptible
    machine_type    = "n1-standard-2"
    service_account = google_service_account.default.email
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform"
    ]
  }
}
