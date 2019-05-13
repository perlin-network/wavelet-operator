.PHONY: aws

docker_aws:
	$(shell aws ecr get-login --no-include-email)
	operator-sdk build 010313437810.dkr.ecr.us-east-2.amazonaws.com/perlin/wavelet-operator
	docker push 010313437810.dkr.ecr.us-east-2.amazonaws.com/perlin/wavelet-operator
	sed -i 's|REPLACE_IMAGE|010313437810.dkr.ecr.us-east-2.amazonaws.com/perlin/wavelet-operator|g' deploy/operator.yaml

docker_hub:
	operator-sdk build perlin/wavelet:operator
	docker push perlin/wavelet:operator
	sed -i 's|REPLACE_IMAGE|perlin/wavelet:operator|g' deploy/operator.yaml

setup:
	kubectl apply -f deploy/service_account.yaml
	kubectl apply -f deploy/role.yaml
	kubectl apply -f deploy/role_binding.yaml
	kubectl apply -f deploy/crds/wavelet_v1alpha1_wavelet_crd.yaml
	kubectl apply -f deploy/operator.yaml
	kubectl apply -f deploy/crds/wavelet_v1alpha1_wavelet_cr.yaml

delete:
	kubectl delete -f deploy/crds/wavelet_v1alpha1_wavelet_cr.yaml
	kubectl delete -f deploy/operator.yaml
	kubectl delete -f deploy/role.yaml
	kubectl delete -f deploy/role_binding.yaml
	kubectl delete -f deploy/service_account.yaml
	kubectl delete -f deploy/crds/wavelet_v1alpha1_wavelet_crd.yaml

update:
	kubectl apply -f deploy/crds/wavelet_v1alpha1_wavelet_cr.yaml