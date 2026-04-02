#!/bin/bash

# Image Promotion Script
# This script promotes container images from CI/CD registry to production registry
# It handles both the image and its signature

set -e  # Exit on error

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored messages
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Function to display usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Promote container images from CI/CD registry to production registry.

OPTIONS:
    -s, --source-image          Source image (e.g., icr.io/ai-services-cicd/tools:0.7)
    -d, --dest-image            Destination image (e.g., icr.io/ai-services/tools:0.7)
    -u, --username              Registry username (default: iamapikey)
    -p, --password              Registry password (required)
    --on-conflict <action>      Action when destination exists: prompt (default), override, fail
    --authenticated-verify      Use authentication for signature verification
                                (required only when destination is in private registry)
    -h, --help                  Display this help message

EXAMPLES:
    # Promote an image with credentials (prompts if exists)
    $0 -s icr.io/ai-services-cicd/tools:0.7 \\
       -d icr.io/ai-services/tools:0.7 \\
       -p YOUR_API_KEY

    # Force override existing image without prompt
    $0 -s icr.io/ai-services-cicd/tools:0.7 \\
       -d icr.io/ai-services/tools:0.7 \\
       -p YOUR_API_KEY --on-conflict override

    # Fail immediately if destination exists (for CI/CD pipelines)
    $0 -s icr.io/ai-services-cicd/tools:0.7 \\
       -d icr.io/ai-services/tools:0.7 \\
       -p YOUR_API_KEY --on-conflict fail

    # Verify with authentication (for private registry destinations)
    $0 -s icr.io/ai-services-cicd/tools:0.7 \\
       -d icr.io/ai-services/tools:0.7 \\
       -p YOUR_API_KEY --authenticated-verify

    # Using environment variables
    export REGISTRY_PASSWORD=YOUR_API_KEY
    $0 -s icr.io/ai-services-cicd/tools:0.7 \\
       -d icr.io/ai-services/tools:0.7

EOF
    exit 1
}

# Default values
USERNAME="iamapikey"
PASSWORD=""
SOURCE_IMAGE=""
DEST_IMAGE=""
ON_CONFLICT="prompt"  # Options: prompt, override, fail
AUTHENTICATED_VERIFY=false  # Use authentication for signature verification

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -s|--source-image)
            SOURCE_IMAGE="$2"
            shift 2
            ;;
        -d|--dest-image)
            DEST_IMAGE="$2"
            shift 2
            ;;
        -u|--username)
            USERNAME="$2"
            shift 2
            ;;
        -p|--password)
            PASSWORD="$2"
            shift 2
            ;;
        --on-conflict)
            ON_CONFLICT="$2"
            if [[ ! "$ON_CONFLICT" =~ ^(prompt|override|fail)$ ]]; then
                print_error "Invalid --on-conflict value: $ON_CONFLICT (must be: prompt, override, or fail)"
                usage
            fi
            shift 2
            ;;
        --authenticated-verify)
            AUTHENTICATED_VERIFY=true
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            ;;
    esac
done

# Check for required parameters
if [ -z "$SOURCE_IMAGE" ]; then
    print_error "Source image is required"
    usage
fi

if [ -z "$DEST_IMAGE" ]; then
    print_error "Destination image is required"
    usage
fi

# Check for password in environment variable if not provided
if [ -z "$PASSWORD" ]; then
    if [ -n "$REGISTRY_PASSWORD" ]; then
        PASSWORD="$REGISTRY_PASSWORD"
    else
        print_error "Registry password is required (use -p or set REGISTRY_PASSWORD environment variable)"
        exit 1
    fi
fi

# Check if required tools are installed
check_dependencies() {
    local missing_tools=()
    
    if ! command -v skopeo &> /dev/null; then
        missing_tools+=("skopeo")
    fi
    
    if ! command -v cosign &> /dev/null; then
        missing_tools+=("cosign")
    fi
    
    if [ ${#missing_tools[@]} -gt 0 ]; then
        print_error "Missing required tools: ${missing_tools[*]}"
        print_info "Please install the missing tools before running this script"
        exit 1
    fi
}

# Step 1: Check signature for source image
check_signature() {
    print_info "Checking signature for source image: $SOURCE_IMAGE"
    
    if cosign tree --registry-username="$USERNAME" --registry-password="$PASSWORD" "$SOURCE_IMAGE"; then
        print_success "Signature verification completed for source image"
    else
        print_warning "No signature found or verification failed for source image"
        read -p "Do you want to continue without signature? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_info "Promotion cancelled"
            exit 0
        fi
    fi
}

# Step 2: Copy the image
copy_image() {
    print_info "Copying image from $SOURCE_IMAGE to $DEST_IMAGE"
    
    # Check if destination image already exists to avoid accidentally overriding
    print_info "Checking if destination image already exists"
    if skopeo inspect --creds "$USERNAME:$PASSWORD" "docker://$DEST_IMAGE" &> /dev/null; then
        print_warning "Destination image already exists: $DEST_IMAGE"
        
        case "$ON_CONFLICT" in
            fail)
                print_error "Destination image exists and --on-conflict is set to 'fail'"
                print_error "Terminating to prevent accidental override"
                exit 1
                ;;
            override)
                print_warning "Destination image exists but --on-conflict is set to 'override'"
                print_warning "Proceeding with override as specified"
                ;;
            prompt)
                print_info "This check prevents accidentally overriding an existing image"
                read -p "Do you want to override the existing image? (y/N): " -n 1 -r
                echo
                if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                    print_info "Image promotion cancelled to prevent override"
                    exit 0
                fi
                print_warning "Proceeding with override as confirmed by user"
                ;;
        esac
    else
        print_info "Destination image does not exist, proceeding with copy"
    fi
    
    if skopeo copy --all \
        --src-creds "$USERNAME:$PASSWORD" \
        --dest-creds "$USERNAME:$PASSWORD" \
        "docker://$SOURCE_IMAGE" \
        "docker://$DEST_IMAGE"; then
        print_success "Image copied successfully"
    else
        print_error "Failed to copy image"
        exit 1
    fi
}

# Step 3: Copy the signature
copy_signature() {
    print_info "Extracting signature reference from source image"
    
    # Get the signature reference from cosign tree output
    local sig_output
    sig_output=$(cosign tree --registry-username="$USERNAME" --registry-password="$PASSWORD" "$SOURCE_IMAGE" 2>&1 || true)
    
    # Extract the signature tag (format: sha256-HASH.sig)
    local sig_tag
    sig_tag=$(echo "$sig_output" | grep -oE 'sha256-[a-f0-9]+\.sig' | head -1)
    
    if [ -z "$sig_tag" ]; then
        print_warning "No signature found for source image, skipping signature copy"
        return 0
    fi
    
    # Extract registry and image name from source and destination
    local src_registry_image="${SOURCE_IMAGE%:*}"
    local dest_registry_image="${DEST_IMAGE%:*}"
    
    local src_sig="$src_registry_image:$sig_tag"
    local dest_sig="$dest_registry_image:$sig_tag"
    
    print_info "Copying signature from $src_sig to $dest_sig"
    
    if skopeo copy --all \
        --src-creds "$USERNAME:$PASSWORD" \
        --dest-creds "$USERNAME:$PASSWORD" \
        "docker://$src_sig" \
        "docker://$dest_sig"; then
        print_success "Signature copied successfully"
    else
        print_warning "Failed to copy signature (this may be expected if no signature exists)"
    fi
}

# Step 4: Verify the promoted image
verify_promoted_image() {
    print_info "Verifying promoted image: $DEST_IMAGE"
    
    # Build cosign command with optional authentication
    local cosign_cmd="cosign tree"
    if [ "$AUTHENTICATED_VERIFY" = true ]; then
        print_info "Using authenticated verification for private registry"
        cosign_cmd="$cosign_cmd --registry-username=\"$USERNAME\" --registry-password=\"$PASSWORD\""
    fi
    
    if eval "$cosign_cmd \"$DEST_IMAGE\""; then
        print_success "Promoted image verification completed"
    else
        print_warning "Verification completed with warnings (signature may not be present)"
    fi
}

# Main execution
main() {
    print_info "Starting image promotion process"
    echo "Source: $SOURCE_IMAGE"
    echo "Destination: $DEST_IMAGE"
    echo ""
    
    # Check dependencies
    check_dependencies
    
    # Execute promotion steps
    check_signature
    echo ""
    
    copy_image
    echo ""
    
    copy_signature
    echo ""
    
    verify_promoted_image
    echo ""
    
    print_success "Image promotion completed successfully!"
    print_info "Promoted image: $DEST_IMAGE"
}

# Run main function
main

# Made with Bob
